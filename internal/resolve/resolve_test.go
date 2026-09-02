package resolve

import (
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/model"
)

// project builds a live Project with the given key and slug. Every other field
// is filled to satisfy model validation; resolution never reads them.
func project(key model.ProjectKey, slug string) model.Project {
	return model.Project{
		Key:             key,
		Slug:            slug,
		CreatedAt:       model.Timestamp("2026-09-02T00:00:00.000000000Z"),
		UpdatedAt:       model.Timestamp("2026-09-02T00:00:00.000000000Z"),
		CreationReplica: model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Revision:        model.Revision{Generation: 1, Replica: model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}
}

// Keys used across the table tests. Real 26-character Base32 keys, so a
// Registration or Found result can be compared against a literal.
const (
	dropsKey  = model.ProjectKey("bbbbbbbbbbbbbbbbbbbbbbbbbb")
	beaconKey = model.ProjectKey("cccccccccccccccccccccccccc")
	notesKey  = model.ProjectKey("dddddddddddddddddddddddddd")
)

func reserved() []model.Project {
	return []model.Project{
		project(model.GlobalProjectKey, model.GlobalProjectSlug),
		project(model.InboxProjectKey, model.InboxProjectSlug),
	}
}

// TestInvocationRulesWinFirst pins the two invocation-only rules and their
// order against every ambient fact: a -P flag and $DROPS_PROJECT both name a
// Project outright, and neither consults the directory.
func TestInvocationRulesWinFirst(t *testing.T) {
	ambient := Request{
		Cwd:      "/home/ted/src/drops",
		Git:      gitFacts("/home/ted/src/drops", "git@github.com:tedkulp/drops.git"),
		Projects: append(reserved(), project(dropsKey, "drops"), project(beaconKey, "beacon")),
		Bindings: []model.WorkspaceBinding{{Path: "/home/ted/src/drops", ProjectKey: dropsKey}},
	}

	tests := []struct {
		name string
		flag string
		env  string
		want model.ProjectKey
		rule Rule
	}{
		{
			name: "flag names a Project the directory does not",
			flag: "beacon",
			want: beaconKey,
			rule: RuleProjectFlag,
		},
		{
			name: "environment names a Project the directory does not",
			env:  "beacon",
			want: beaconKey,
			rule: RuleEnvProject,
		},
		{
			name: "flag beats environment",
			flag: "beacon",
			env:  "global",
			want: beaconKey,
			rule: RuleProjectFlag,
		},
		{
			name: "environment reaches a reserved Project",
			env:  model.GlobalProjectSlug,
			want: model.GlobalProjectKey,
			rule: RuleEnvProject,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := ambient
			req.ProjectFlag = test.flag
			req.EnvProject = test.env

			result, err := Resolve(req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != Found {
				t.Fatalf("outcome = %v, want %v", result.Outcome, Found)
			}
			if result.Project != test.want {
				t.Errorf("project = %q, want %q", result.Project, test.want)
			}
			if result.Rule != test.rule {
				t.Errorf("rule = %v, want %v", result.Rule, test.rule)
			}
			if result.Register != (Registration{}) {
				t.Errorf("register = %+v, want empty: an invocation rule never binds", result.Register)
			}
		})
	}
}

// TestUnknownInvocationSlugIsNotFound pins the exit-code contract: a name the
// caller typed that no Project answers is ErrNotFound, never a silent fallback
// to the ambient directory.
func TestUnknownInvocationSlugIsNotFound(t *testing.T) {
	ambient := Request{
		Cwd:      "/home/ted/src/drops",
		Git:      gitFacts("/home/ted/src/drops", ""),
		Projects: append(reserved(), project(dropsKey, "drops")),
		Bindings: []model.WorkspaceBinding{{Path: "/home/ted/src/drops", ProjectKey: dropsKey}},
	}

	for _, test := range []struct {
		name string
		req  Request
	}{
		{name: "flag", req: func() Request { r := ambient; r.ProjectFlag = "nope"; return r }()},
		{name: "environment", req: func() Request { r := ambient; r.EnvProject = "nope"; return r }()},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(test.req)
			if !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("err = %v, want model.ErrNotFound", err)
			}
		})
	}
}

// gitFacts builds the repository facts gitx.Inspect would report for a
// directory inside root.
func gitFacts(root, remote string) gitx.Facts {
	if root == "" {
		return gitx.Facts{}
	}
	return gitx.Facts{InRepository: true, Root: root, RemoteURL: remote}
}

// TestGitRootBindingWins pins that a Git-backed lookup is keyed on the main
// repository root, never on the working directory. A linked worktree reports
// the main root, so every worktree of a repository shares its one binding, and
// a binding recorded against a subdirectory of a repository is not an exact
// Git-root match.
func TestGitRootBindingWins(t *testing.T) {
	tests := []struct {
		name     string
		cwd      string
		root     string
		bindings []model.WorkspaceBinding
		want     Outcome
		project  model.ProjectKey
	}{
		{
			name:     "cwd deep inside the repository",
			cwd:      "/home/ted/src/drops/internal/resolve",
			root:     "/home/ted/src/drops",
			bindings: []model.WorkspaceBinding{{Path: "/home/ted/src/drops", ProjectKey: dropsKey}},
			want:     Found,
			project:  dropsKey,
		},
		{
			name: "linked worktree resolves through the main root",
			cwd:  "/home/ted/worktrees/drops-feature/internal",
			root: "/home/ted/src/drops",
			bindings: []model.WorkspaceBinding{
				{Path: "/home/ted/src/drops", ProjectKey: dropsKey},
			},
			want:    Found,
			project: dropsKey,
		},
		{
			name: "a binding on the worktree path is not the Git root",
			cwd:  "/home/ted/worktrees/drops-feature/internal",
			root: "/home/ted/src/drops",
			bindings: []model.WorkspaceBinding{
				{Path: "/home/ted/worktrees/drops-feature", ProjectKey: beaconKey},
			},
			want: Unregistered,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      test.cwd,
				Git:      gitFacts(test.root, ""),
				Projects: append(reserved(), project(dropsKey, "drops"), project(beaconKey, "beacon")),
				Bindings: test.bindings,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != test.want {
				t.Fatalf("outcome = %v, want %v", result.Outcome, test.want)
			}
			if test.want != Found {
				return
			}
			if result.Project != test.project {
				t.Errorf("project = %q, want %q", result.Project, test.project)
			}
			if result.Rule != RuleWorkspaceBinding {
				t.Errorf("rule = %v, want %v", result.Rule, RuleWorkspaceBinding)
			}
		})
	}
}

// TestNormalizeLocator pins the reduction that lets one repository answer to
// one Project however it was cloned. Every want is written off the rule
// "host/owner/name, lowercased, scheme, credentials, port and .git removed",
// not read back from the implementation.
func TestNormalizeLocator(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "scp form", raw: "git@github.com:tedkulp/drops.git", want: "github.com/tedkulp/drops"},
		{name: "https form", raw: "https://github.com/tedkulp/drops.git", want: "github.com/tedkulp/drops"},
		{name: "https without suffix", raw: "https://github.com/tedkulp/drops", want: "github.com/tedkulp/drops"},
		{name: "trailing slash", raw: "https://github.com/tedkulp/drops/", want: "github.com/tedkulp/drops"},
		{name: "embedded credentials", raw: "https://user:token@github.com/TedKulp/Drops.git", want: "github.com/tedkulp/drops"},
		{name: "ssh scheme", raw: "ssh://git@github.com/tedkulp/drops.git", want: "github.com/tedkulp/drops"},
		{name: "ssh scheme with port", raw: "ssh://git@github.com:22/tedkulp/drops.git", want: "github.com/tedkulp/drops"},
		{name: "nested groups", raw: "git@gitlab.com:group/sub/drops.git", want: "gitlab.com/group/sub/drops"},
		{name: "surrounding whitespace", raw: "  git@github.com:tedkulp/drops.git\n", want: "github.com/tedkulp/drops"},
		{name: "empty", raw: "", want: ""},
		{name: "no owner segment", raw: "https://github.com/drops.git", want: ""},
		{name: "prose is not a remote", raw: "not a remote url", want: ""},
		{name: "absolute path is machine-local", raw: "/home/ted/src/mirrors/drops.git", want: ""},
		{name: "file scheme is machine-local", raw: "file:///home/ted/src/mirrors/drops.git", want: ""},
		{name: "relative path is machine-local", raw: "../mirrors/drops.git", want: ""},
		{name: "absolute path with a colon is still a path", raw: "/home/ted/a:b/mirrors/drops.git", want: ""},
		{name: "relative path with a colon is still a path", raw: "./a:b/mirrors/drops.git", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeLocator(test.raw); got != test.want {
				t.Errorf("NormalizeLocator(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

// locator builds a live repository locator for a Project.
func locator(key model.ProjectKey, value string) model.RepositoryLocator {
	return model.RepositoryLocator{
		ProjectKey: key,
		Locator:    value,
		Tombstone:  model.Live,
		Revision:   model.Revision{Generation: 1, Replica: model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}
}

// TestLocatorRule pins discovery by normalized origin: it runs only after the
// exact Git-root binding, it ignores tombstoned evidence, and a locator two
// Projects claim is reported as ambiguous rather than ordered.
func TestLocatorRule(t *testing.T) {
	const remote = "git@github.com:tedkulp/drops.git"
	tombstoned := locator(beaconKey, "github.com/tedkulp/drops")
	tombstoned.Tombstone = model.Tombstoned

	tests := []struct {
		name     string
		remote   string
		bindings []model.WorkspaceBinding
		locators []model.RepositoryLocator
		want     Outcome
		project  model.ProjectKey
		rule     Rule
	}{
		{
			name:     "unique live locator answers a clone in a new directory",
			remote:   remote,
			locators: []model.RepositoryLocator{locator(dropsKey, "github.com/tedkulp/drops")},
			want:     Found,
			project:  dropsKey,
			rule:     RuleRepositoryLocator,
		},
		{
			name:     "an exact binding beats a locator naming another Project",
			remote:   remote,
			bindings: []model.WorkspaceBinding{{Path: "/home/ted/clones/drops", ProjectKey: beaconKey}},
			locators: []model.RepositoryLocator{locator(dropsKey, "github.com/tedkulp/drops")},
			want:     Found,
			project:  beaconKey,
			rule:     RuleWorkspaceBinding,
		},
		{
			name:   "two Projects claiming one locator is ambiguous",
			remote: remote,
			locators: []model.RepositoryLocator{
				locator(dropsKey, "github.com/tedkulp/drops"),
				locator(beaconKey, "github.com/tedkulp/drops"),
			},
			want: Ambiguous,
		},
		{
			name:   "one Project claiming a locator twice is not ambiguous",
			remote: remote,
			locators: []model.RepositoryLocator{
				locator(dropsKey, "github.com/tedkulp/drops"),
				locator(dropsKey, "github.com/tedkulp/drops"),
			},
			want:    Found,
			project: dropsKey,
			rule:    RuleRepositoryLocator,
		},
		{
			name:     "a tombstoned locator is not evidence",
			remote:   remote,
			locators: []model.RepositoryLocator{tombstoned},
			want:     Unregistered,
		},
		{
			name:     "a machine-local remote is never a locator",
			remote:   "/home/ted/src/mirrors/drops.git",
			locators: []model.RepositoryLocator{locator(dropsKey, "home/ted/src/mirrors/drops")},
			want:     Unregistered,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      "/home/ted/clones/drops",
				Git:      gitFacts("/home/ted/clones/drops", test.remote),
				Projects: append(reserved(), project(dropsKey, "drops"), project(beaconKey, "beacon")),
				Bindings: test.bindings,
				Locators: test.locators,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != test.want {
				t.Fatalf("outcome = %v, want %v", result.Outcome, test.want)
			}
			if test.want != Found {
				return
			}
			if result.Project != test.project || result.Rule != test.rule {
				t.Errorf("got %q by %v, want %q by %v", result.Project, result.Rule, test.project, test.rule)
			}
		})
	}
}

// TestAmbiguousLocatorNamesEveryClaimant pins that the caller can report which
// Projects collided without querying for them again, in an order that does not
// depend on how the store happened to return the rows.
func TestAmbiguousLocatorNamesEveryClaimant(t *testing.T) {
	result, err := Resolve(Request{
		Cwd:      "/home/ted/clones/drops",
		Git:      gitFacts("/home/ted/clones/drops", "git@github.com:tedkulp/drops.git"),
		Projects: append(reserved(), project(dropsKey, "drops"), project(beaconKey, "beacon")),
		Locators: []model.RepositoryLocator{
			locator(beaconKey, "github.com/tedkulp/drops"),
			locator(dropsKey, "github.com/tedkulp/drops"),
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Ambiguity.Kind != AmbiguousLocator {
		t.Fatalf("kind = %v, want %v", result.Ambiguity.Kind, AmbiguousLocator)
	}
	if result.Ambiguity.Locator != "github.com/tedkulp/drops" {
		t.Errorf("locator = %q, want the normalized form", result.Ambiguity.Locator)
	}
	want := []model.ProjectKey{dropsKey, beaconKey} // sorted by key, not row order
	if len(result.Ambiguity.Projects) != len(want) {
		t.Fatalf("projects = %v, want %v", result.Ambiguity.Projects, want)
	}
	for i, key := range want {
		if result.Ambiguity.Projects[i] != key {
			t.Errorf("projects[%d] = %q, want %q", i, result.Ambiguity.Projects[i], key)
		}
	}
}

// TestWorkspacePrefixRule pins the last ambient rule. It routes a directory
// that is not itself a registered Git root, matches only on whole path
// components, and takes the longest binding when several cover the directory.
//
// The one case it must refuse is a distinct repository nested under an
// unrelated binding: an unbound nested Git root beats an ancestor binding, or
// every issue written from a checkout inside a bound workspace is silently
// misfiled into that workspace's Project.
func TestWorkspacePrefixRule(t *testing.T) {
	bindings := []model.WorkspaceBinding{
		{Path: "/home/ted/notes", ProjectKey: notesKey},
		{Path: "/home/ted/notes/drops", ProjectKey: dropsKey},
	}

	tests := []struct {
		name     string
		cwd      string
		root     string
		bindings []model.WorkspaceBinding
		want     Outcome
		project  model.ProjectKey
	}{
		{
			name:     "non-Git directory under a binding",
			cwd:      "/home/ted/notes/daily/2026",
			bindings: bindings,
			want:     Found,
			project:  notesKey,
		},
		{
			name:     "the binding path itself",
			cwd:      "/home/ted/notes",
			bindings: bindings,
			want:     Found,
			project:  notesKey,
		},
		{
			name:     "the longest binding wins",
			cwd:      "/home/ted/notes/drops/spec",
			bindings: bindings,
			want:     Found,
			project:  dropsKey,
		},
		{
			name:     "a shared textual prefix is not a path prefix",
			cwd:      "/home/ted/notesx/daily",
			bindings: bindings,
			want:     Unregistered,
		},
		{
			name:     "no binding covers the directory",
			cwd:      "/home/ted/scratch",
			bindings: bindings,
			want:     Unregistered,
		},
		{
			name:     "an unbound nested Git root beats an ancestor binding",
			cwd:      "/home/ted/notes/clone/internal",
			root:     "/home/ted/notes/clone",
			bindings: bindings,
			want:     Unregistered,
		},
		{
			name:     "a binding inside the repository still routes",
			cwd:      "/home/ted/src/drops/notes/daily",
			root:     "/home/ted/src/drops",
			bindings: []model.WorkspaceBinding{{Path: "/home/ted/src/drops/notes", ProjectKey: notesKey}},
			want:     Found,
			project:  notesKey,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      test.cwd,
				Git:      gitFacts(test.root, ""),
				Projects: append(reserved(), project(dropsKey, "drops"), project(notesKey, "notes")),
				Bindings: test.bindings,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != test.want {
				t.Fatalf("outcome = %v, want %v", result.Outcome, test.want)
			}
			if test.want != Found {
				return
			}
			if result.Project != test.project {
				t.Errorf("project = %q, want %q", result.Project, test.project)
			}
			if result.Rule != RuleWorkspacePrefix {
				t.Errorf("rule = %v, want %v", result.Rule, RuleWorkspacePrefix)
			}
		})
	}
}

// TestWriteRegistration pins what a trusted write is told to persist. Reads
// never create or bind, so the same directory yields an empty Registration
// under Read and the full one under Write.
func TestWriteRegistration(t *testing.T) {
	const root = "/home/ted/clones/drops"

	tests := []struct {
		name     string
		intent   Intent
		remote   string
		bindings []model.WorkspaceBinding
		locators []model.RepositoryLocator
		want     Outcome
		project  model.ProjectKey
		rule     Rule
		register Registration
	}{
		{
			name:   "a write in an unclaimed repository proposes the whole registration",
			intent: Write,
			remote: "git@github.com:tedkulp/drops.git",
			want:   Unregistered,
			register: Registration{
				CreateProject: true,
				Slug:          "drops",
				Locator:       "github.com/tedkulp/drops",
				BindingPath:   root,
			},
		},
		{
			name:   "a read in the same repository proposes nothing",
			intent: Read,
			remote: "git@github.com:tedkulp/drops.git",
			want:   Unregistered,
		},
		{
			name:     "a write matched by locator learns the path and not the origin",
			intent:   Write,
			remote:   "git@github.com:tedkulp/drops.git",
			locators: []model.RepositoryLocator{locator(dropsKey, "github.com/tedkulp/drops")},
			want:     Found,
			project:  dropsKey,
			rule:     RuleRepositoryLocator,
			register: Registration{BindingPath: root},
		},
		{
			name:     "a read matched by locator is told to persist nothing",
			intent:   Read,
			remote:   "git@github.com:tedkulp/drops.git",
			locators: []model.RepositoryLocator{locator(dropsKey, "github.com/tedkulp/drops")},
			want:     Found,
			project:  dropsKey,
			rule:     RuleRepositoryLocator,
		},
		{
			name:     "a write matched by an existing binding persists nothing",
			intent:   Write,
			remote:   "git@github.com:tedkulp/drops.git",
			bindings: []model.WorkspaceBinding{{Path: root, ProjectKey: dropsKey}},
			want:     Found,
			project:  dropsKey,
			rule:     RuleWorkspaceBinding,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      root + "/internal",
				Intent:   test.intent,
				Git:      gitFacts(root, test.remote),
				Projects: append(reserved(), project(beaconKey, "beacon")),
				Bindings: test.bindings,
				Locators: test.locators,
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != test.want || result.Rule != test.rule || result.Project != test.project {
				t.Fatalf("got %v %v %q, want %v %v %q",
					result.Outcome, result.Rule, result.Project, test.want, test.rule, test.project)
			}
			if result.Register != test.register {
				t.Errorf("register = %+v, want %+v", result.Register, test.register)
			}
		})
	}
}

// TestOutsideAnyContext pins the last rule: with no Git repository and no
// binding there is nothing to name a Project, so a read stays empty and a write
// files under the reserved inbox Project rather than inventing one.
func TestOutsideAnyContext(t *testing.T) {
	for _, test := range []struct {
		name    string
		intent  Intent
		want    Outcome
		project model.ProjectKey
		rule    Rule
	}{
		{name: "read", intent: Read, want: Unregistered},
		{name: "write", intent: Write, want: Found, project: model.InboxProjectKey, rule: RuleInboxFallback},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      "/tmp/scratch",
				Intent:   test.intent,
				Projects: reserved(),
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != test.want || result.Project != test.project || result.Rule != test.rule {
				t.Fatalf("got %v %v %q, want %v %v %q",
					result.Outcome, result.Rule, result.Project, test.want, test.rule, test.project)
			}
			if result.Register != (Registration{}) {
				t.Errorf("register = %+v, want empty: a directory in no repository is never bound", result.Register)
			}
		})
	}
}

// TestReservedSlugsAreKnown pins that the reserved names are resolve's own
// constants, exactly as the inbox fallback key is. A Request whose Projects
// happen not to carry them must still not propose one, or a repository
// directory called inbox or global mints a second Project under a slug the two
// stores are supposed to converge on.
func TestReservedSlugsAreKnown(t *testing.T) {
	for _, slug := range []string{model.InboxProjectSlug, model.GlobalProjectSlug} {
		t.Run(slug, func(t *testing.T) {
			root := "/home/ted/clones/" + slug
			result, err := Resolve(Request{
				Cwd:    root,
				Intent: Write,
				Git:    gitFacts(root, ""),
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.Outcome != Ambiguous || result.Ambiguity.Kind != AmbiguousSlug {
				t.Fatalf("got %v/%v, want %v/%v",
					result.Outcome, result.Ambiguity.Kind, Ambiguous, AmbiguousSlug)
			}
		})
	}
}

// TestProposedSlug pins how an auto-created Project is named: the bare
// repository name, then owner-name, then nothing. A numeric suffix is never a
// candidate, and the reserved slugs are never proposed, so a repository
// directory called inbox cannot capture the fallback Project.
func TestProposedSlug(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		remote  string
		taken   []model.Project
		want    string
		ambigue bool
	}{
		{
			name:   "the bare repository name",
			root:   "/home/ted/clones/drops",
			remote: "git@github.com:tedkulp/drops.git",
			want:   "drops",
		},
		{
			name:   "owner-name disambiguates a taken name",
			root:   "/home/ted/clones/drops",
			remote: "git@github.com:tedkulp/drops.git",
			taken:  []model.Project{project(beaconKey, "drops")},
			want:   "tedkulp-drops",
		},
		{
			name:   "both candidates taken is ambiguous",
			root:   "/home/ted/clones/drops",
			remote: "git@github.com:tedkulp/drops.git",
			taken: []model.Project{
				project(beaconKey, "drops"),
				project(notesKey, "tedkulp-drops"),
			},
			ambigue: true,
		},
		{
			name: "no remote falls back to the directory name",
			root: "/home/ted/clones/Scratch Pad",
			want: "scratch-pad",
		},
		{
			name:    "a reserved slug is never proposed",
			root:    "/home/ted/clones/inbox",
			ambigue: true,
		},
		{
			name:    "the filesystem root names nothing",
			root:    "/",
			ambigue: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Resolve(Request{
				Cwd:      test.root,
				Intent:   Write,
				Git:      gitFacts(test.root, test.remote),
				Projects: append(reserved(), test.taken...),
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if test.ambigue {
				if result.Outcome != Ambiguous {
					t.Fatalf("outcome = %v, want %v", result.Outcome, Ambiguous)
				}
				if result.Ambiguity.Kind != AmbiguousSlug {
					t.Errorf("kind = %v, want %v", result.Ambiguity.Kind, AmbiguousSlug)
				}
				return
			}
			if result.Outcome != Unregistered {
				t.Fatalf("outcome = %v, want %v", result.Outcome, Unregistered)
			}
			if result.Register.Slug != test.want {
				t.Errorf("slug = %q, want %q", result.Register.Slug, test.want)
			}
		})
	}
}

// TestInvalidRequest pins that resolution refuses inputs it cannot compare.
// Every path rule is lexical, so a caller that has not made its paths absolute
// would otherwise get a silent miss rather than an error.
func TestInvalidRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		req  Request
	}{
		{name: "empty working directory", req: Request{}},
		{name: "relative working directory", req: Request{Cwd: "src/drops"}},
		{name: "relative repository root", req: Request{Cwd: "/home/ted", Git: gitx.Facts{InRepository: true, Root: "../drops"}}},
		{name: "repository without a root", req: Request{Cwd: "/home/ted", Git: gitx.Facts{InRepository: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := test.req
			req.Projects = reserved()
			if _, err := Resolve(req); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("err = %v, want model.ErrInvalid", err)
			}
		})
	}
}

// TestResolveReadsNothingAmbient pins that every fact arrives in the Request.
// $DROPS_PROJECT is passed in as EnvProject; the process environment itself is
// never consulted, so a resolution is reproducible from its inputs alone.
func TestResolveReadsNothingAmbient(t *testing.T) {
	t.Setenv("DROPS_PROJECT", "beacon")

	result, err := Resolve(Request{
		Cwd:      "/home/ted/src/drops",
		Git:      gitFacts("/home/ted/src/drops", ""),
		Projects: append(reserved(), project(dropsKey, "drops"), project(beaconKey, "beacon")),
		Bindings: []model.WorkspaceBinding{{Path: "/home/ted/src/drops", ProjectKey: dropsKey}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Project != dropsKey {
		t.Fatalf("project = %q, want %q: the environment was read behind the Request", result.Project, dropsKey)
	}
}
