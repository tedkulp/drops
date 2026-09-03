package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// testedVerbs is the registry the census enforces: every command in the cobra
// tree, against the test that asserts what docs/agents/issue-tracker.md says a
// reader observes from it.
//
// It is a visible list on purpose. The failure this guards against is not a
// verb nobody thought to test — it is a verb ADDED later and silently left
// untested, which no coverage number distinguishes from a verb whose tests all
// exercise its flags and never its observable. Adding a command now means
// editing this map, and the census fails until you do.
//
// The value is the test name so the map doubles as an index: given a command,
// it says where its contract is written down.
var testedVerbs = map[string]string{
	"drops":                 "TestBareRootPrintsHelpAndSucceeds",
	"drops blocked":         "TestBlockedRowNamesItsBlockers",
	"drops close":           "TestCloseRecordsItsReasonOnTheIssue",
	"drops claim":           "TestClaimAssignsOnlyAnUnclaimedIssue",
	"drops comment":         "TestBareGroupPrintsHelpAndSucceeds",
	"drops comment add":     "TestCommentAddPrintsTheCommentID",
	"drops comment list":    "TestCommentListRendersTheThreadOldestFirst",
	"drops comment rm":      "TestCommentRmDropsOneCommentFromTheThread",
	"drops config":          "TestBareGroupPrintsHelpAndSucceeds",
	"drops config show":     "TestConfigShowNamesTheStoreAndTheResolvedProject",
	"drops count":           "TestCountAnswersTheScopeAndFilters",
	"drops create":          "TestCreateFilesUnderItsParentsProject",
	"drops dep":             "TestBareGroupPrintsHelpAndSucceeds",
	"drops dep add":         "TestDepAddMakesTheBlockedIssueUnready",
	"drops dep cycles":      "TestDepCyclesReportsAndExitsOne",
	"drops dep rm":          "TestDepRmRestoresReadiness",
	"drops dep tree":        "TestDepTreePrintsTheChainIndented",
	"drops doctor":          "TestDoctorExitsOneOnACredentialFinding",
	"drops forget":          "TestForgetTombstonesAndHidesTheMemory",
	"drops label":           "TestBareGroupPrintsHelpAndSucceeds",
	"drops label add":       "TestLabelAddAndRmChangeTheIssuesLabels",
	"drops label list":      "TestLabelListCountsAreProjectScoped",
	"drops label rm":        "TestLabelAddAndRmChangeTheIssuesLabels",
	"drops list":            "TestListRowByteExact",
	"drops memories":        "TestMemoriesReadSpansEveryProject",
	"drops memory":          "TestBareGroupPrintsHelpAndSucceeds",
	"drops memory edit":     "TestMemoryEditRefusesToSplitASupersessionChain",
	"drops memory show":     "TestMemoryShowPrintsTitleLineThenBody",
	"drops move":            "TestMoveReportsTheEdgesItMadeCross",
	"drops project":         "TestBareGroupPrintsHelpAndSucceeds",
	"drops project add":     "TestProjectAddRegistersASlug",
	"drops project archive": "TestProjectArchiveHidesItFromListAndRefusesNewIssues",
	"drops project list":    "TestProjectListMapsKeyToSlug",
	"drops project rename":  "TestProjectRenameKeepsTheProjectKey",
	"drops q":               "TestQuickCapturePrintsOnlyTheID",
	"drops ready":           "TestReadyRowByteExact",
	"drops remember":        "TestRememberFilesUnderTheResolvedProject",
	"drops reopen":          "TestReopenClearsTheCloseReason",
	"drops release":         "TestReleaseClearsTheClaim",
	"drops replica":         "TestBareGroupPrintsHelpAndSucceeds",
	"drops replica rekey":   "TestReplicaRekeyRotatesTheSidecarKey",
	"drops search":          "TestSearchHonoursAllFlag",
	"drops show":            "TestShowJSONShape",
	"drops supersede":       "TestSupersedeRetiresTheOldMemoryAndInheritsItsProject",
	"drops sync":            "TestSyncWithoutARemoteIsAnError",
	"drops update":          "TestUpdateChangesOnlyTheFlagsYouPass",
	"drops version":         "TestVersionWorksBeforeTheStoreExists",
}

// commandPaths walks the whole tree, root included, and returns every command
// path. Help and completion are excluded: neither is drops' contract, and the
// completion command is disabled outright.
func commandPaths(t *testing.T) []string {
	t.Helper()
	root, cleanup := NewRootCmd(Options{})
	defer cleanup()

	var paths []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Name() == "help" || cmd.Name() == cobra.ShellCompRequestCmd {
			return
		}
		paths = append(paths, cmd.CommandPath())
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
	sort.Strings(paths)
	return paths
}

// censusFindings is the check itself, over data rather than over the live
// registry: given the tree's command paths, a registry and the set of tests
// that actually exist, it returns one line per problem and nothing when there
// is none.
//
// It takes its inputs as arguments because a check whose inputs are always
// correct can never be observed failing — with a sound registry, disabling the
// check and leaving it in place produce identical output. Here it can be handed
// a stale entry and asked what it says.
func censusFindings(paths []string, registry map[string]string, declared map[string]bool) []string {
	var findings []string
	inTree := map[string]bool{}
	for _, path := range paths {
		inTree[path] = true
		if _, ok := registry[path]; !ok {
			findings = append(findings, fmt.Sprintf(
				"%q is in the command tree and not in testedVerbs: give it a boundary test "+
					"asserting what the doc says a reader observes, then add it here", path))
		}
	}
	for path, name := range registry {
		if !inTree[path] {
			findings = append(findings, fmt.Sprintf(
				"testedVerbs names %q, which is not in the command tree: a renamed or removed "+
					"verb leaves a registry entry that proves nothing", path))
		}
		if !declared[name] {
			findings = append(findings, fmt.Sprintf(
				"testedVerbs[%q] names test %q, which no _test.go file in this package declares",
				path, name))
		}
	}
	sort.Strings(findings)
	return findings
}

// TestCensusFindingsCatchesEachWayARegistryRots is the check under test. Each
// case is one way the registry stops proving anything, and every one of them is
// silent against the live registry precisely because the live registry is
// sound.
func TestCensusFindingsCatchesEachWayARegistryRots(t *testing.T) {
	paths := []string{"drops", "drops list"}
	registry := map[string]string{"drops": "TestRoot", "drops list": "TestList"}
	declared := map[string]bool{"TestRoot": true, "TestList": true}

	if got := censusFindings(paths, registry, declared); len(got) != 0 {
		t.Fatalf("a sound registry produced findings: %v", got)
	}

	for _, testcase := range []struct {
		name     string
		paths    []string
		registry map[string]string
		declared map[string]bool
		want     string
	}{
		{
			name:     "a command nobody registered",
			paths:    []string{"drops", "drops list", "drops brandnew"},
			registry: registry, declared: declared,
			want: "is in the command tree and not in testedVerbs",
		},
		{
			name:  "an entry for a command that is gone",
			paths: paths,
			registry: map[string]string{
				"drops": "TestRoot", "drops list": "TestList", "drops renamed": "TestRoot",
			},
			declared: declared,
			want:     "which is not in the command tree",
		},
		{
			name:     "an entry naming a test that does not exist",
			paths:    paths,
			registry: registry,
			declared: map[string]bool{"TestRoot": true},
			want:     "which no _test.go file in this package declares",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			got := censusFindings(testcase.paths, testcase.registry, testcase.declared)
			if len(got) != 1 || !strings.Contains(got[0], testcase.want) {
				t.Fatalf("findings = %#v, want exactly one mentioning %q", got, testcase.want)
			}
		})
	}
}

// TestEveryCommandIsInTheTestedVerbRegistry is the census: the same check over
// the real tree, the real registry and the tests this package really declares.
// It is what fails when a verb is added and left untested.
func TestEveryCommandIsInTheTestedVerbRegistry(t *testing.T) {
	for _, finding := range censusFindings(commandPaths(t), testedVerbs, declaredTestNames(t)) {
		t.Error(finding)
	}
}

// declaredTestNames parses this package's own _test.go files and returns every
// `func TestX(*testing.T)` it declares. Reflection cannot answer this: the test
// binary knows the tests it was built to run, not the names in the source.
func declaredTestNames(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse this package's tests: %v", err)
	}
	names := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
					names[fn.Name.Name] = true
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("parsed no test declarations at all; the census would pass vacuously")
	}
	return names
}
