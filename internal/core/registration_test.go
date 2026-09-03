package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// The clause (dw32p.8): registering a Project writes the Project, its shared
// repository locator and its LOCAL workspace binding as one unit, and the
// binding is the part that never syncs.
func TestRegisterProjectWritesProjectLocatorAndBindingTogether(t *testing.T) {
	rules, _ := openCore(t)

	project, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "drops", Locator: "github.com/tedkulp/drops", BindingPath: "/home/ted/src/drops",
	})
	if err != nil {
		t.Fatalf("register Project: %v", err)
	}

	locators, err := rules.ProjectLocators(t.Context(), project.Key)
	if err != nil || len(locators) != 1 || locators[0].Locator != "github.com/tedkulp/drops" {
		t.Fatalf("locators = %#v, %v", locators, err)
	}
	bindings, err := rules.WorkspaceBindings(t.Context())
	if err != nil || len(bindings) != 1 ||
		bindings[0].Path != "/home/ted/src/drops" || bindings[0].ProjectKey != project.Key {
		t.Fatalf("bindings = %#v, %v", bindings, err)
	}
	byLocator, err := rules.ProjectsByLocator(t.Context(), "github.com/tedkulp/drops")
	if err != nil || len(byLocator) != 1 || byLocator[0].ProjectKey != project.Key {
		t.Fatalf("by locator = %#v, %v", byLocator, err)
	}
}

// The clause: a registration carrying NO evidence is a plain Project create,
// so a taken slug is a conflict rather than a silent hand-back.
//
// Convergence is what a registration does with evidence to attach: the loser
// puts its locator and binding on the winner and returns it, which is the right
// answer because both sessions were describing the same repository. With
// nothing to attach there is no such argument, and returning a Project the
// caller never named would look like it had created one.
func TestRegisterProjectWithNoEvidenceConflictsOnATakenSlug(t *testing.T) {
	rules, _ := openCore(t)

	project, err := rules.RegisterProject(t.Context(), core.Registration{Slug: "bare"})
	if err != nil {
		t.Fatalf("register bare Project: %v", err)
	}
	if project.Slug != "bare" {
		t.Fatalf("project = %#v", project)
	}
	bindings, err := rules.WorkspaceBindings(t.Context())
	if err != nil || len(bindings) != 0 {
		t.Fatalf("bindings = %#v, %v, want none", bindings, err)
	}

	second, err := rules.RegisterProject(t.Context(), core.Registration{Slug: "bare"})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("re-registering a taken slug = %#v, %v, want ErrConflict", second, err)
	}
}

// The clause: the reserved slugs are not registrable, so no discovered
// directory can mint a second `global` or `inbox`.
func TestRegisterProjectRefusesReservedAndEmptySlugs(t *testing.T) {
	rules, _ := openCore(t)

	for _, slug := range []string{"", "global", "inbox"} {
		_, err := rules.RegisterProject(t.Context(), core.Registration{
			Slug: slug, Locator: "example.com/x", BindingPath: "/workspace/x",
		})
		if !errors.Is(err, model.ErrInvalid) {
			t.Errorf("register %q: err = %v, want ErrInvalid", slug, err)
		}
	}
}

// The clause: a registration that LOSES a same-slug race converges on the
// winner — it returns the winning Project and attaches its own evidence there,
// rather than failing or minting a duplicate.
//
// Two agent sessions entering the same repository at once is the ordinary case
// this covers, so a refusal here would be a routine failure.
func TestRegisterProjectConvergesOnTheWinnerOfASameSlugRace(t *testing.T) {
	rules, _ := openCore(t)

	winner, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "drops", Locator: "github.com/tedkulp/drops", BindingPath: "/home/ted/src/drops",
	})
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}

	// The loser arrives with its own evidence: a second checkout of the same
	// repository, and a locator the winner does not carry yet.
	loser, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "drops", Locator: "git.example.com/drops", BindingPath: "/home/ted/worktrees/drops",
	})
	if err != nil {
		t.Fatalf("losing registration: %v", err)
	}
	if loser.Key != winner.Key {
		t.Fatalf("loser key = %s, want the winner %s", loser.Key, winner.Key)
	}

	projects, err := rules.Projects(t.Context())
	if err != nil {
		t.Fatalf("read Projects: %v", err)
	}
	drops := 0
	for _, project := range projects {
		if project.Slug == "drops" {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("%d Projects named drops, want exactly one", drops)
	}

	locators, err := rules.ProjectLocators(t.Context(), winner.Key)
	if err != nil || len(locators) != 2 {
		t.Fatalf("locators = %#v, %v, want both attached to the winner", locators, err)
	}
	bindings, err := rules.WorkspaceBindings(t.Context())
	if err != nil || len(bindings) != 2 {
		t.Fatalf("bindings = %#v, %v, want both attached to the winner", bindings, err)
	}
	for _, binding := range bindings {
		if binding.ProjectKey != winner.Key {
			t.Errorf("binding %s points at %s, want the winner %s", binding.Path, binding.ProjectKey, winner.Key)
		}
	}
}

// The clause: a locator the winner already carries is not written twice. The
// convergence path re-runs for every losing session, so without this check a
// busy repository accumulates a duplicate locator per race.
func TestRegisterProjectDoesNotReAddALocatorTheWinnerHas(t *testing.T) {
	rules, _ := openCore(t)

	winner, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "drops", Locator: "github.com/tedkulp/drops", BindingPath: "/home/ted/src/drops",
	})
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if _, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "drops", Locator: "github.com/tedkulp/drops", BindingPath: "/home/ted/src/drops",
	}); err != nil {
		t.Fatalf("second registration: %v", err)
	}

	locators, err := rules.ProjectLocators(t.Context(), winner.Key)
	if err != nil || len(locators) != 1 {
		t.Fatalf("locators = %#v, %v, want the one locator unduplicated", locators, err)
	}
}

// The clause (dw32p.8): a repository locator is SHARED discovery evidence, so
// removing one tombstones it — leaving a record for the other machine to
// merge — rather than deleting the row.
func TestSetRepositoryLocatorTombstonesRatherThanDeletes(t *testing.T) {
	rules, _ := openCore(t)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}

	added, err := rules.SetRepositoryLocator(t.Context(), project.Key, "github.com/tedkulp/drops", true)
	if err != nil {
		t.Fatalf("add locator: %v", err)
	}
	if added.Tombstone != model.Live || added.Revision.Generation != 1 {
		t.Fatalf("added = %#v, want a live generation 1 locator", added)
	}

	removed, err := rules.SetRepositoryLocator(t.Context(), project.Key, "github.com/tedkulp/drops", false)
	if err != nil {
		t.Fatalf("remove locator: %v", err)
	}
	if removed.Tombstone != model.Tombstoned || removed.Revision.Generation != 2 {
		t.Fatalf("removed = %#v, want a tombstoned generation 2 locator", removed)
	}
	// Tombstoned, so it no longer answers discovery...
	matches, err := rules.ProjectsByLocator(t.Context(), "github.com/tedkulp/drops")
	if err != nil || len(matches) != 0 {
		t.Fatalf("by locator = %#v, %v, want no live match", matches, err)
	}
	// ... but the record is still there to be restored and re-exported.
	restored, err := rules.SetRepositoryLocator(t.Context(), project.Key, "github.com/tedkulp/drops", true)
	if err != nil {
		t.Fatalf("restore locator: %v", err)
	}
	if restored.Tombstone != model.Live || restored.Revision.Generation != 3 {
		t.Fatalf("restored = %#v, want a live generation 3 locator", restored)
	}
}

// The clause: setting a locator to the state it already holds is a no-op, so a
// repeated discovery does not export as an edit.
func TestSetRepositoryLocatorIsIdempotent(t *testing.T) {
	rules, _ := openCore(t)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	added, err := rules.SetRepositoryLocator(t.Context(), project.Key, "github.com/tedkulp/drops", true)
	if err != nil {
		t.Fatalf("add locator: %v", err)
	}

	again, err := rules.SetRepositoryLocator(t.Context(), project.Key, "github.com/tedkulp/drops", true)
	if err != nil {
		t.Fatalf("re-add locator: %v", err)
	}
	if again.Revision != added.Revision {
		t.Fatalf("re-add revision = %#v, want %#v left alone", again.Revision, added.Revision)
	}
}

// The clause: removing a locator that was never there is not-found, and an
// empty locator is invalid input that says it is EMPTY. Neither writes anything.
//
// ErrInvalid alone cannot carry the empty case: model.RepositoryLocator.Validate
// also refuses it, one transaction later, as `repository locator locator ""`.
// So the assertion is on the word the agent needs to read.
func TestSetRepositoryLocatorRefusesEmptyAndAbsentLocators(t *testing.T) {
	rules, _ := openCore(t)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}

	_, empty := rules.SetRepositoryLocator(t.Context(), project.Key, "", true)
	if !errors.Is(empty, model.ErrInvalid) {
		t.Errorf("empty locator error = %v, want ErrInvalid", empty)
	}
	if empty == nil || !strings.Contains(empty.Error(), "empty") {
		t.Errorf("empty locator refusal does not say it is empty: %v", empty)
	}
	if _, err := rules.SetRepositoryLocator(t.Context(), project.Key, "never/seen", false); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("removing an absent locator error = %v, want ErrNotFound", err)
	}
	locators, err := rules.ProjectLocators(t.Context(), project.Key)
	if err != nil || len(locators) != 0 {
		t.Fatalf("locators = %#v, %v, want none written", locators, err)
	}
}

// The clause: a locator belongs to a Project, so one naming no Project is
// refused by the explicit read rather than left to the foreign key.
//
// The same trap as AddComment: internal/store maps constraint 787 to
// ErrNotFound, so without the read the agent gets "constraint failed: FOREIGN
// KEY constraint failed (787)" and the error class alone proves nothing.
func TestSetRepositoryLocatorRefusesAnUnknownProject(t *testing.T) {
	rules, _ := openCore(t)
	absent, err := model.NewProjectKey()
	if err != nil {
		t.Fatalf("mint key: %v", err)
	}
	_, refusal := rules.SetRepositoryLocator(t.Context(), absent, "github.com/tedkulp/drops", true)
	if !errors.Is(refusal, model.ErrNotFound) {
		t.Fatalf("locator on unknown Project error = %v, want ErrNotFound", refusal)
	}
	if strings.Contains(refusal.Error(), "constraint") {
		t.Errorf("refusal leaks SQLite's constraint text: %v", refusal)
	}
}

// The clause (dw32p.8): a workspace binding is LOCAL — it is deleted outright
// rather than tombstoned, because it never crosses the mirror seam and so has
// no other machine to tell.
func TestUnbindWorkspaceDeletesRatherThanTombstones(t *testing.T) {
	rules, _ := openCore(t)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	if err := rules.BindWorkspace(t.Context(), model.WorkspaceBinding{
		Path: "/home/ted/src/drops", ProjectKey: project.Key,
	}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if err := rules.UnbindWorkspace(t.Context(), "/home/ted/src/drops"); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	bindings, err := rules.WorkspaceBindings(t.Context())
	if err != nil || len(bindings) != 0 {
		t.Fatalf("bindings = %#v, %v, want the row gone", bindings, err)
	}
}

// The clause: a binding points at a Project, so binding a directory to a
// Project that does not exist is refused rather than written.
func TestBindWorkspaceRefusesAnUnknownProject(t *testing.T) {
	rules, _ := openCore(t)
	absent, err := model.NewProjectKey()
	if err != nil {
		t.Fatalf("mint key: %v", err)
	}

	err = rules.BindWorkspace(t.Context(), model.WorkspaceBinding{
		Path: "/home/ted/src/nowhere", ProjectKey: absent,
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("bind to unknown Project error = %v, want ErrNotFound", err)
	}
	// As everywhere else here, the foreign key would also produce an
	// ErrNotFound, so the error class alone cannot prove the read happened.
	if strings.Contains(err.Error(), "constraint") {
		t.Errorf("refusal leaks SQLite's constraint text: %v", err)
	}
	bindings, readErr := rules.WorkspaceBindings(t.Context())
	if readErr != nil || len(bindings) != 0 {
		t.Fatalf("bindings = %#v, %v, want none written", bindings, readErr)
	}
}
