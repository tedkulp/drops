package cli

import (
	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/view"
)

// showRefJSON is one related issue in show --json: the same three fields the
// old build emitted, so a consumer's `.children[].title` keeps resolving.
type showRefJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// showIssueJSON is the flat --json shape of show: the issue's own fields, the
// live label names, and the relation keys that never go absent.
type showIssueJSON struct {
	model.Issue
	Labels         []string        `json:"labels,omitempty"`
	Parent         *showRefJSON    `json:"parent"`
	Blockers       []showRefJSON   `json:"blockers"`
	Blocking       []showRefJSON   `json:"blocking"`
	Children       []showRefJSON   `json:"children"`
	DiscoveredFrom []showRefJSON   `json:"discovered_from"`
	Discovered     []showRefJSON   `json:"discovered"`
	Related        []showRefJSON   `json:"related"`
	Comments       []model.Comment `json:"comments"`
}

// assembleView turns one view into the page and the JSON object show renders.
// The relations arrive from core already named and already ordered, read in the
// same transaction as the issue itself, so nothing here reads the store.
//
// The JSON is derived from the PAGE rather than from the view a second time.
// There is one adapter (internal/view) and the machine surface reads its
// output, so the two renderings cannot disagree about which edges block, which
// relations are live, or what order they come in — they are the same slices.
func assembleView(issueView core.IssueView) (render.Page, showIssueJSON) {
	page := view.Page(issueView)
	out := showIssueJSON{
		Issue:          issueView.Issue,
		Labels:         page.Labels,
		Blockers:       toJSONRefs(page.Blockers),
		Blocking:       toJSONRefs(page.Blocking),
		Children:       toJSONRefs(page.Children),
		DiscoveredFrom: toJSONRefs(page.DiscoveredFrom),
		Discovered:     toJSONRefs(page.Discovered),
		Related:        toJSONRefs(page.Related),
		Comments:       issueView.Comments,
	}
	if page.Parent != nil {
		parent := toJSONRef(*page.Parent)
		out.Parent = &parent
	}
	return page, out
}

func toJSONRef(ref render.Ref) showRefJSON {
	return showRefJSON{ID: string(ref.ID), Title: ref.Title, Status: string(ref.Status)}
}

func toJSONRefs(refs []render.Ref) []showRefJSON {
	out := make([]showRefJSON, 0, len(refs))
	for _, ref := range refs {
		out = append(out, toJSONRef(ref))
	}
	return out
}

// projectRoutingJSON is one project in `project list --json`: the Project
// record, then the two tables the resolution ladder consults to decide which
// directory reaches it.
//
// The two have different lifetimes, and showing them together is the point.
// A repository locator is a replicated record: it carries a revision, it
// travels in the mirror, and every machine sees the same one. A workspace
// binding is machine-local: it holds one machine's absolute path, has no
// revision, and appears in no snapshot. Anyone reading this to debug a
// two-machine routing problem needs to know which of the two facts travelled,
// so the rows say it — a revision is present on exactly the one that does.
//
// Neither key ever goes absent. A project with no bindings answers `[]`, so a
// parser's `.workspace_bindings[]` is safe on every project rather than most.
type projectRoutingJSON struct {
	model.Project
	WorkspaceBindings  []model.WorkspaceBinding  `json:"workspace_bindings"`
	RepositoryLocators []model.RepositoryLocator `json:"repository_locators"`
}

// assembleProjects hangs each project's routing rows on it.
//
// The two arrive differently and that is why only one is grouped here. Every
// machine-local binding comes back from one store-wide read, so splitting them
// by project is a map build; locators are read per project and arrive already
// grouped.
func assembleProjects(
	projects []model.Project,
	bindings []model.WorkspaceBinding,
	locators map[model.ProjectKey][]model.RepositoryLocator,
) []projectRoutingJSON {
	bound := map[model.ProjectKey][]model.WorkspaceBinding{}
	for _, binding := range bindings {
		bound[binding.ProjectKey] = append(bound[binding.ProjectKey], binding)
	}
	out := make([]projectRoutingJSON, 0, len(projects))
	for _, project := range projects {
		out = append(out, projectRoutingJSON{
			Project:            project,
			WorkspaceBindings:  bound[project.Key],
			RepositoryLocators: locators[project.Key],
		})
	}
	return out
}
