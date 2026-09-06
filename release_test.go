package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The release configuration is checked from here, in package main, for the same
// reason TestTheTreeCompilesForDarwin is: the defect class is one no unit test
// can reach. A release config drifts silently — a renamed stamp variable, a
// files: entry naming a file somebody deleted, a goos this tree cannot honour —
// and the first report is a failed tag or, worse, a published binary that says
// `devel`.
//
// Two things about the shape of these tests.
//
// They read the files as text rather than as YAML, because a structural parse
// would mean a dependency in go.mod for four files this repository owns and
// whose shape it fixes. The parsing here is a dozen lines and every test names
// what it assumes.
//
// And they are NOT the real reader. AGENTS.md asks that a serialized contract
// be proved by running the real writer and decoding its bytes; the real reader
// of .goreleaser.yaml is `goreleaser check`, which is a tool this repository
// does not depend on and so cannot be in the gate. It runs in CI instead, on
// every push. That is a knowing exception rather than an oversight: the local
// gate can accept a config goreleaser would reject, and CI is what closes it.
//
// Note for running the controls these prove: the root package's path is `./.`,
// so `just mutate .` matches every package in the tree rather than this one.
// Scope them by control name.
const (
	releaseConfig   = ".goreleaser.yaml"
	ciWorkflow      = ".github/workflows/ci.yml"
	releaseWorkflow = ".github/workflows/release.yml"
	justfileName    = "justfile"
)

// stampedVariable matches one `-X importpath.Ident=value` linker stamp, in
// either the goreleaser ldflags list or the justfile's build recipe. The value
// runs to the end of the line, because the justfile's is a command
// substitution containing spaces.
var stampedVariable = regexp.MustCompile(`-X ([^\s=]+)\.([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// stamp is one `-X` linker stamp, kept apart rather than as one "path.Ident"
// string, so that asking for the package, the variable or the value is a field
// access instead of a second parse of the same text.
type stamp struct {
	importPath string
	name       string
	value      string
}

func (s stamp) symbol() string { return s.importPath + "." + s.name }

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// contentLines is every line of a file that is not blank and not a comment,
// paired with nothing else. `#` is the comment character in YAML and in a
// justfile alike, and dropping those lines is load-bearing rather than tidy:
// both files explain their own configuration in prose directly above it, so a
// scan that read the comments would find what the file SAYS it does and pass a
// file that had been changed to do something else.
func contentLines(source string) []string {
	var out []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// modulePath is what go.mod declares, so the stamp check can tell a package in
// this module from one anywhere else.
func modulePath(t *testing.T) string {
	t.Helper()
	for _, line := range contentLines(readRepoFile(t, "go.mod")) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod declares no module path")
	return ""
}

// stamps returns every `-X` in source, sorted by symbol and deduplicated.
func stamps(source string) []stamp {
	seen := map[string]stamp{}
	for _, line := range contentLines(source) {
		for _, m := range stampedVariable.FindAllStringSubmatch(line, -1) {
			// A justfile recipe continues across lines; the trailing
			// backslash is the shell's, not part of the value.
			value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[3]), `\`))
			s := stamp{importPath: m[1], name: m[2], value: value}
			seen[s.symbol()] = s
		}
	}
	out := make([]stamp, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].symbol() < out[j].symbol() })
	return out
}

func symbols(in []stamp) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, s.symbol())
	}
	return out
}

// stampOf returns the stamp of the named symbol, failing if it is absent.
func stampOf(t *testing.T, in []stamp, symbol string) stamp {
	t.Helper()
	for _, s := range in {
		if s.symbol() == symbol {
			return s
		}
	}
	t.Fatalf("no -X stamp for %s among %v", symbol, symbols(in))
	return stamp{}
}

// declaresVar reports whether dir's package declares name as a package-level
// var. It is the question the stamp actually depends on: `go build -X` writes
// to a package-level string var and says nothing at all when the symbol is
// absent.
func declaresVar(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, ident := range value.Names {
					if ident.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}

// indentOf is the column a line's content starts at, which is what opens and
// closes a block in both of the file formats read here.
func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// blockAfter returns the lines following lines[i] that are indented past it,
// trimmed, with blanks and comments dropped. It is the one walk both of the
// readers below need — a YAML list under a key, and the body of a `run: |`
// block are the same shape.
func blockAfter(lines []string, i int) []string {
	indent := indentOf(lines[i])
	var out []string
	for _, next := range lines[i+1:] {
		trimmed := strings.TrimSpace(next)
		if trimmed == "" {
			continue
		}
		if indentOf(next) <= indent {
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// yamlListsUnder returns every `- item` list nested under a line whose trimmed
// text is exactly `key:`. A non-list line ends the list, so a key whose block
// holds a mapping yields what preceded it rather than that mapping's contents.
func yamlListsUnder(source, key string) [][]string {
	var lists [][]string
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != key+":" {
			continue
		}
		var items []string
		for _, entry := range blockAfter(lines, i) {
			item, ok := strings.CutPrefix(entry, "- ")
			if !ok {
				break
			}
			items = append(items, strings.Trim(strings.TrimSpace(item), `"'`))
		}
		lists = append(lists, items)
	}
	return lists
}

// runSteps returns the shell each `run:` step in a workflow executes, one line
// per command, taking a block scalar's body as well as the single-line form.
//
// It is deliberately narrower than a scan of the whole file. A workflow says
// the gate's name twice over — in the prose above the step and in the job's
// display name — and neither of those runs anything, so a check that read them
// would pass a workflow whose actual command had been swapped for something
// weaker.
func runSteps(t *testing.T, workflow string) []string {
	t.Helper()
	var out []string
	lines := strings.Split(readRepoFile(t, workflow), "\n")
	for i, line := range lines {
		trimmed := strings.TrimPrefix(strings.TrimSpace(line), "- ")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		rest, isRun := strings.CutPrefix(trimmed, "run:")
		if !isRun {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest != "" && !strings.HasPrefix(rest, "|") && !strings.HasPrefix(rest, ">") {
			out = append(out, rest)
			continue
		}
		out = append(out, blockAfter(lines, i)...)
	}
	return out
}

// TestTheReleaseStampsVariablesThisTreeDeclares is the guard on the one release
// failure that is silent. `go build -X missing.Symbol=v` is not an error: the
// stamp is dropped and the binary ships its compile-time default, so renaming
// cli.BuildDate would publish a release whose `drops version` says "devel" with
// nothing anywhere going red.
func TestTheReleaseStampsVariablesThisTreeDeclares(t *testing.T) {
	module := modulePath(t)
	stamped := stamps(readRepoFile(t, releaseConfig))
	if len(stamped) == 0 {
		t.Fatalf("%s stamps nothing; a release built from it reports devel", releaseConfig)
	}
	for _, s := range stamped {
		rel, inModule := strings.CutPrefix(s.importPath, module)
		if !inModule {
			t.Errorf("%s stamps %s, which is not in module %s", releaseConfig, s.symbol(), module)
			continue
		}
		dir := strings.TrimPrefix(rel, "/")
		if dir == "" {
			dir = "."
		}
		if !declaresVar(t, dir, s.name) {
			t.Errorf("%s stamps %s but package %s declares no var %s; the stamp is dropped silently and the binary reports its default", releaseConfig, s.symbol(), dir, s.name)
		}
	}
}

// TestTheReleaseAndTheBuildRecipeStampTheSameVariables holds the two build
// paths to one answer. `just build` and the release build are the only two
// things that produce a stamped drops, and a variable added to one and not the
// other is a binary whose provenance depends on who built it.
func TestTheReleaseAndTheBuildRecipeStampTheSameVariables(t *testing.T) {
	fromRelease := symbols(stamps(readRepoFile(t, releaseConfig)))
	fromRecipe := symbols(stamps(readRepoFile(t, justfileName)))
	if strings.Join(fromRelease, " ") != strings.Join(fromRecipe, " ") {
		t.Errorf("%s stamps %v, the justfile's build recipe stamps %v; the two builds disagree about a released binary's provenance", releaseConfig, fromRelease, fromRecipe)
	}
}

// TestTheReleaseStampsAVPrefixedVersion is the other half of that agreement,
// and it is a separate clause because the variable NAMES can match while the
// values disagree. GoReleaser's {{.Version}} is the tag with its leading `v`
// stripped, while `just build` stamps `git describe` verbatim and this
// repository's tags are `v*` — so without the `v` written back on, a released
// binary would say `drops 0.1.0` where a local one says `drops
// v0.1.0-14-gabc1234`, and the same commit would have two spellings.
func TestTheReleaseStampsAVPrefixedVersion(t *testing.T) {
	version := stampOf(t, stamps(readRepoFile(t, releaseConfig)), "github.com/tedkulp/drops/internal/cli.Version")
	if !strings.HasPrefix(version.value, "v") {
		t.Errorf("%s stamps Version as %q; goreleaser strips the tag's leading v, so a released binary would spell the tag differently from `just build`", releaseConfig, version.value)
	}
}

// TestTheReleaseBuildsOnlyTheOperatingSystemsThisTreeSupports pairs with
// TestTheTreeCompilesForDarwin. drops is UNIX-only by design — flock, ioctl —
// and darwin is the one other GOOS this tree is cross-vetted for, so those two
// are exactly what a release may publish. A third would ship a binary nothing
// here has ever compiled.
func TestTheReleaseBuildsOnlyTheOperatingSystemsThisTreeSupports(t *testing.T) {
	lists := yamlListsUnder(readRepoFile(t, releaseConfig), "goos")
	if len(lists) != 1 {
		t.Fatalf("%s declares %d goos lists, want exactly 1", releaseConfig, len(lists))
	}
	got := append([]string(nil), lists[0]...)
	sort.Strings(got)
	if want := "darwin linux"; strings.Join(got, " ") != want {
		t.Errorf("%s builds for %v, want [%s]; this tree is cross-vetted for darwin alone and is UNIX-only", releaseConfig, lists[0], want)
	}
}

// TestTheReleaseBuildsBothArchitectures is a separate clause from the goos one
// because README, CHANGELOG and the release skill all promise four archives,
// and dropping an arch keeps every one of those documents true-looking while
// publishing half of what they describe. arm64 is the half that would go: it is
// the one nothing in CI builds on.
func TestTheReleaseBuildsBothArchitectures(t *testing.T) {
	lists := yamlListsUnder(readRepoFile(t, releaseConfig), "goarch")
	if len(lists) != 1 {
		t.Fatalf("%s declares %d goarch lists, want exactly 1", releaseConfig, len(lists))
	}
	got := append([]string(nil), lists[0]...)
	sort.Strings(got)
	if want := "amd64 arm64"; strings.Join(got, " ") != want {
		t.Errorf("%s builds for %v, want [%s]; the README, the changelog and the release skill all promise both", releaseConfig, lists[0], want)
	}
}

// TestTheReleaseShipsOnlyFilesThatExist guards a failure that arrives at tag
// time, after the tag is already pushed: goreleaser aborts on a files: entry it
// cannot find, so a deleted LICENSE turns the next release into a rollback.
func TestTheReleaseShipsOnlyFilesThatExist(t *testing.T) {
	lists := yamlListsUnder(readRepoFile(t, releaseConfig), "files")
	if len(lists) == 0 {
		t.Fatalf("%s ships no files: alongside the binary", releaseConfig)
	}
	for _, list := range lists {
		for _, name := range list {
			if _, err := os.Stat(name); err != nil {
				t.Errorf("%s ships %q, which is not in this tree: the release aborts after the tag is pushed", releaseConfig, name)
			}
		}
	}
}

// TestTheReleaseRunsOnAVersionTag holds the trigger. Everything else here is
// about what a release publishes; this is whether one happens at all, and a
// trigger that no longer matches `v*` is silent — pushing the tag simply does
// nothing, with no failed run to notice.
func TestTheReleaseRunsOnAVersionTag(t *testing.T) {
	lists := yamlListsUnder(readRepoFile(t, releaseWorkflow), "tags")
	if len(lists) != 1 {
		t.Fatalf("%s declares %d tag filters, want exactly 1", releaseWorkflow, len(lists))
	}
	if want := []string{"v*"}; strings.Join(lists[0], " ") != strings.Join(want, " ") {
		t.Errorf("%s triggers on %v, want %v; the release skill and the changelog both say a `v*` tag publishes", releaseWorkflow, lists[0], want)
	}
}

// TestTheReleaseCheckoutFetchesFullHistory covers the step that is load-bearing
// twice over and looks like boilerplate both times. A shallow checkout gives
// goreleaser no tag history to build a changelog from, and gives the Go
// toolchain no repository to embed vcs.revision from — which is where `drops
// version` reads the commit, since nothing stamps it by hand.
func TestTheReleaseCheckoutFetchesFullHistory(t *testing.T) {
	depth := false
	for _, line := range contentLines(readRepoFile(t, releaseWorkflow)) {
		if strings.TrimSpace(line) == "fetch-depth: 0" {
			depth = true
		}
	}
	if !depth {
		t.Errorf("%s checks out without `fetch-depth: 0`; goreleaser gets no tag history and the binary gets no vcs.revision, so `drops version` loses the commit", releaseWorkflow)
	}
}

// TestCIRunsTheGateRecipe and TestNoWorkflowRespellsTheGate are two clauses,
// kept as two tests so a mutation names which of them went red.
//
// The gate is `just test-all`: vet over the packages `./...` skips, a gofmt
// check over what git counts, then the race suite with DROPS_DB pointed at the
// tripwire.
func TestCIRunsTheGateRecipe(t *testing.T) {
	for _, line := range runSteps(t, ciWorkflow) {
		if strings.Contains(line, "just test-all") {
			return
		}
	}
	t.Errorf("%s runs no `just test-all` step, so it is not running this repository's gate", ciWorkflow)
}

// TestNoWorkflowRespellsTheGate is the other half. A workflow that wrote the
// gate's steps out itself would be a second spelling free to drift from the
// justfile, and a hand-rolled `go test` step drops the DROPS_DB tripwire
// silently — which is the defect class AGENTS.md exists to refuse.
func TestNoWorkflowRespellsTheGate(t *testing.T) {
	for _, workflow := range []string{ciWorkflow, releaseWorkflow} {
		for _, line := range runSteps(t, workflow) {
			if strings.Contains(line, "go test") {
				t.Errorf("%s spells the gate itself (%q); it must call the justfile recipe, which cannot drift from it", workflow, line)
			}
		}
	}
}
