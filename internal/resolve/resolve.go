package resolve

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/model"
)

// Intent says whether the command being resolved reads or writes. Reads never
// create or bind, so three of the ordered rules answer differently for each.
type Intent int

const (
	Read Intent = iota
	Write
)

// Outcome is the class of one resolution.
type Outcome int

const (
	// Found means one Project answers this invocation.
	Found Outcome = iota + 1
	// Unregistered means nothing answers it. Register says what a write
	// should record about the directory; it is empty for a read.
	Unregistered
	// Ambiguous means the evidence names more than one answer and the
	// caller must fail loudly rather than pick.
	Ambiguous
)

// Rule names the ordered rule that produced a Found result.
type Rule int

const (
	RuleNone Rule = iota
	RuleProjectFlag
	RuleEnvProject
	RuleWorkspaceBinding
	RuleRepositoryLocator
	RuleWorkspacePrefix
	RuleInboxFallback
)

// String names the rung for a report. The names are the ladder's own, so a
// reader who has `config show`'s answer in front of them can find the rule it
// came from in docs/cli-contract.md without a translation table.
//
// RuleNone is the empty string rather than a name, because a rung that did not
// answer is a fact the caller reports by omitting the field.
func (rule Rule) String() string {
	switch rule {
	case RuleProjectFlag:
		return "project-flag"
	case RuleEnvProject:
		return "env-project"
	case RuleWorkspaceBinding:
		return "workspace-binding"
	case RuleRepositoryLocator:
		return "repository-locator"
	case RuleWorkspacePrefix:
		return "workspace-prefix"
	case RuleInboxFallback:
		return "inbox-fallback"
	}
	return ""
}

// AmbiguityKind says which evidence named more than one answer.
type AmbiguityKind int

const (
	NotAmbiguous AmbiguityKind = iota
	// AmbiguousLocator: this checkout's origin is claimed by several Projects.
	AmbiguousLocator
	// AmbiguousSlug: every name this repository could be registered under is
	// already taken by another Project.
	AmbiguousSlug
)

// Ambiguity is the detail behind an Ambiguous outcome, enough for the caller
// to name the collision without asking the store again.
type Ambiguity struct {
	Kind    AmbiguityKind
	Locator string
	// Projects are the claimants, sorted by key so the report is stable.
	Projects []model.ProjectKey
	// Slugs are the names that were tried and found taken.
	Slugs []string
}

// Request is the complete input to one resolution. Nothing is read from the
// process environment, the filesystem, Git, or SQLite: the caller supplies
// every fact, already canonicalized.
type Request struct {
	// Cwd is the absolute, symlink-resolved working directory.
	Cwd string
	// ProjectFlag is the value of -P/--project, empty when not given.
	ProjectFlag string
	// EnvProject is the value of $DROPS_PROJECT, empty when unset.
	EnvProject string
	// Intent is Read unless the command writes.
	Intent Intent
	// Git describes the repository containing Cwd, from gitx.Inspect.
	Git gitx.Facts

	Projects []model.Project
	Bindings []model.WorkspaceBinding
	Locators []model.RepositoryLocator
}

// validate refuses a Request whose paths cannot be compared. Every path rule
// here is lexical, so a caller that has not made its paths absolute and
// symlink-resolved would get a silent miss instead of an answer.
func (req Request) validate() error {
	if !filepath.IsAbs(req.Cwd) {
		return fmt.Errorf("%w: working directory %q is not absolute", model.ErrInvalid, req.Cwd)
	}
	if req.Git.InRepository && !filepath.IsAbs(req.Git.Root) {
		return fmt.Errorf("%w: repository root %q is not absolute", model.ErrInvalid, req.Git.Root)
	}
	return nil
}

// Registration is what a trusted write should persist about this directory.
// Its zero value means nothing is to be recorded.
type Registration struct {
	// CreateProject means no existing Project matched and one must be minted.
	CreateProject bool
	// Slug is the name to mint CreateProject under.
	Slug string
	// Locator is the normalized origin to record as discovery evidence.
	Locator string
	// BindingPath is the machine-local path to bind to the Project.
	BindingPath string
}

// Result is the complete answer to one resolution.
type Result struct {
	Outcome  Outcome
	Rule     Rule
	Project  model.ProjectKey
	Register Registration
	// Ambiguity is set when Outcome is Ambiguous.
	Ambiguity Ambiguity
}

// Resolve applies the ordered rules and reports the first that answers. It is
// a pure function: it decides what a write should persist and never persists it.
func Resolve(req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if slug := req.ProjectFlag; slug != "" {
		return namedProject(req, slug, RuleProjectFlag, "project %q")
	}
	if slug := req.EnvProject; slug != "" {
		return namedProject(req, slug, RuleEnvProject, "DROPS_PROJECT names unknown project %q")
	}
	if req.Git.InRepository {
		if key, ok := binding(req.Bindings, req.Git.Root); ok {
			return Result{Outcome: Found, Rule: RuleWorkspaceBinding, Project: key}, nil
		}
		if value := NormalizeLocator(req.Git.RemoteURL); value != "" {
			claimants := claim(req.Locators, value)
			switch {
			case len(claimants) == 1:
				// A write learns the path, which is machine-local. It never
				// learns the origin: only a trusted add, bind or auto-create
				// may add discovery evidence.
				return Result{
					Outcome: Found, Rule: RuleRepositoryLocator, Project: claimants[0],
					Register: register(req, Registration{BindingPath: req.Git.Root}),
				}, nil
			case len(claimants) > 1:
				return Result{Outcome: Ambiguous, Ambiguity: Ambiguity{
					Kind: AmbiguousLocator, Locator: value, Projects: claimants,
				}}, nil
			}
		}
	}
	if key, path, ok := longestPrefix(req.Bindings, req.Cwd); ok && describes(req.Git, path) {
		return Result{Outcome: Found, Rule: RuleWorkspacePrefix, Project: key}, nil
	}
	return unregistered(req)
}

// unregistered answers a directory no Project claims. A read reports the fact.
// A write in a repository proposes registering it; a write anywhere else has
// nothing to go on and files under the reserved inbox Project.
func unregistered(req Request) (Result, error) {
	if req.Intent != Write {
		return Result{Outcome: Unregistered}, nil
	}
	if !req.Git.InRepository {
		return Result{Outcome: Found, Rule: RuleInboxFallback, Project: model.InboxProjectKey}, nil
	}

	value := NormalizeLocator(req.Git.RemoteURL)
	candidates := SlugCandidates(value, filepath.Base(req.Git.Root))
	for _, slug := range candidates {
		if taken(req.Projects, slug) {
			continue
		}
		return Result{Outcome: Unregistered, Register: Registration{
			CreateProject: true,
			Slug:          slug,
			Locator:       value,
			BindingPath:   req.Git.Root,
		}}, nil
	}
	// Guessing a suffixed slug would register this checkout's path and origin
	// against a name nobody chose, and every later write from the directory
	// would file there silently. Report the collision instead.
	return Result{Outcome: Ambiguous, Ambiguity: Ambiguity{
		Kind: AmbiguousSlug, Locator: value, Slugs: candidates,
	}}, nil
}

// register keeps a Registration only for a write: reads never create or bind.
func register(req Request, proposal Registration) Registration {
	if req.Intent != Write {
		return Registration{}
	}
	return proposal
}

// taken reports whether a slug is unavailable. The reserved slugs are always
// unavailable, whether or not their Projects are present.
func taken(projects []model.Project, slug string) bool {
	if slug == model.GlobalProjectSlug || slug == model.InboxProjectSlug {
		return true
	}
	return slices.ContainsFunc(projects, func(candidate model.Project) bool {
		return candidate.Slug == slug
	})
}

// longestPrefix returns the Project bound to the deepest directory containing
// path. Matching is by whole path component, so /home/ted/notesx is not covered
// by a binding on /home/ted/notes.
func longestPrefix(bindings []model.WorkspaceBinding, path string) (model.ProjectKey, string, bool) {
	var best model.WorkspaceBinding
	for _, candidate := range bindings {
		if !covers(candidate.Path, path) || len(candidate.Path) <= len(best.Path) {
			continue
		}
		best = candidate
	}
	return best.ProjectKey, best.Path, best.Path != ""
}

func covers(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// describes reports whether a binding may route a directory by prefix. Outside
// a repository it always may. Inside one it may only when the bound directory
// lies within that repository, because a repository is its own workspace: an
// unbound nested Git root beats an ancestor binding, and a binding on one
// linked worktree does not speak for the repository the worktree belongs to.
// Either way the alternative is silently misfiling every issue written from a
// checkout that no Project has claimed.
func describes(facts gitx.Facts, bound string) bool {
	return !facts.InRepository || covers(facts.Root, bound)
}

// claim returns every Project holding live evidence for one locator, sorted by
// key and deduplicated: one Project may record the same locator more than once.
func claim(locators []model.RepositoryLocator, value string) []model.ProjectKey {
	var keys []model.ProjectKey
	for _, candidate := range locators {
		if candidate.Locator != value || candidate.Tombstone == model.Tombstoned {
			continue
		}
		if !slices.Contains(keys, candidate.ProjectKey) {
			keys = append(keys, candidate.ProjectKey)
		}
	}
	slices.Sort(keys)
	return keys
}

// binding reports the Project bound to exactly path.
func binding(bindings []model.WorkspaceBinding, path string) (model.ProjectKey, bool) {
	for _, candidate := range bindings {
		if candidate.Path == path {
			return candidate.ProjectKey, true
		}
	}
	return "", false
}

// namedProject answers an invocation-only rule. The name was typed, so a name
// nothing answers is an error rather than a fall-through to the directory.
func namedProject(req Request, slug string, rule Rule, format string) (Result, error) {
	for _, candidate := range req.Projects {
		if candidate.Slug == slug {
			return Result{Outcome: Found, Rule: rule, Project: candidate.Key}, nil
		}
	}
	return Result{}, fmt.Errorf("%s: %w", fmt.Sprintf(format, slug), model.ErrNotFound)
}
