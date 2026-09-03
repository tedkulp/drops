package main

import (
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// A derived key is the whole reason two machines can converge, so the property
// under test is not "it returns something" but "it returns the same thing, and
// that thing is a valid Project key".
func TestProjectKeyForIsStableAndValid(t *testing.T) {
	first, err := ProjectKeyFor("drops")
	if err != nil {
		t.Fatalf("ProjectKeyFor: %v", err)
	}
	second, err := ProjectKeyFor("drops")
	if err != nil {
		t.Fatalf("ProjectKeyFor: %v", err)
	}
	if first != second {
		t.Errorf("ProjectKeyFor(drops) = %q then %q; a derived key must not vary", first, second)
	}
	if err := first.Validate(); err != nil {
		t.Errorf("derived key %q is not a valid Project key: %v", first, err)
	}
	other, err := ProjectKeyFor("beacon")
	if err != nil {
		t.Fatalf("ProjectKeyFor: %v", err)
	}
	if other == first {
		t.Errorf("two slugs derived the same key %q", first)
	}
}

// The pinned value is the contract between machines. A change to the domain
// separator or the encoding would silently give the second machine different
// keys for the same Projects, which no test of stability alone would catch.
func TestProjectKeyForIsPinned(t *testing.T) {
	for slug, want := range map[string]model.ProjectKey{
		"drops":  "h5r6cccdjsp77ofkxrjy3tmryy",
		"beacon": "bu43ax4hjurtyhcmgvnghokqgy",
	} {
		got, err := ProjectKeyFor(slug)
		if err != nil {
			t.Fatalf("ProjectKeyFor(%s): %v", slug, err)
		}
		if got != want {
			t.Errorf("ProjectKeyFor(%s) = %q, want %q", slug, got, want)
		}
	}
}

// internal/store pins global and inbox itself, before it consults the plan. A
// derived key for either would be a second spelling of an identity the model
// already fixes.
func TestProjectKeyForRefusesTheReservedSlugs(t *testing.T) {
	for _, slug := range []string{model.GlobalProjectSlug, model.InboxProjectSlug} {
		if _, err := ProjectKeyFor(slug); err == nil {
			t.Errorf("ProjectKeyFor(%s) succeeded; the reserved slugs take their fixed keys", slug)
		}
	}
	keys, err := ProjectKeys([]string{"drops", model.GlobalProjectSlug, model.InboxProjectSlug, ""})
	if err != nil {
		t.Fatalf("ProjectKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("ProjectKeys assigned %d keys, want only drops: %v", len(keys), keys)
	}
}

func TestNormalizeLocator(t *testing.T) {
	for _, row := range []struct{ remote, want string }{
		{"github.com/tedkulp/tix", "github.com/tedkulp/tix"},
		{"git@github.com:tedkulp/drops.git", "github.com/tedkulp/drops"},
		{"https://github.com/tedkulp/drops.git", "github.com/tedkulp/drops"},
		{"https://user:token@github.com:443/tedkulp/drops", "github.com/tedkulp/drops"},
		{"ssh://git@GitHub.com/Tedkulp/Drops.git", "github.com/Tedkulp/Drops"},
		{"  ", ""},
		{"https://github.com", ""},
		{"/home/ted/src/drops", ""},
	} {
		if got := NormalizeLocator(row.remote); got != row.want {
			t.Errorf("NormalizeLocator(%q) = %q, want %q", row.remote, got, row.want)
		}
	}
}

// A remote with no locator is reported by the conversion rather than guessed
// at, so it must not reach the table at all.
func TestLocatorsOmitsWhatItCannotNormalize(t *testing.T) {
	locators := Locators(map[string]string{
		"tix":   "github.com/tedkulp/tix",
		"local": "/home/ted/src/drops",
	})
	if _, present := locators["local"]; present {
		t.Errorf("Locators kept an unnormalizable remote: %v", locators)
	}
	if locators["tix"] != "github.com/tedkulp/tix" {
		t.Errorf("Locators[tix] = %q", locators["tix"])
	}
}

func TestCurateDropsWhatTheCurationOmitted(t *testing.T) {
	if _, keep := Curate(store.LegacyMemory{ID: "br-dnb", ProjectSlug: "beacon"}); keep {
		t.Error("br-dnb is in the omitted set and was kept")
	}
}

// Ownership is semantic: a subject-specific row filed globally moves to the
// Project it describes, and a row may name a Project the legacy store lacked.
func TestCurateReassignsOwnership(t *testing.T) {
	curated, keep := Curate(store.LegacyMemory{ID: "br-ksvc", ProjectSlug: model.GlobalProjectSlug})
	if !keep {
		t.Fatal("br-ksvc is retained and was dropped")
	}
	if curated.ProjectSlug != "ansible" {
		t.Errorf("br-ksvc landed in %q, want ansible", curated.ProjectSlug)
	}
}

// The tables measured one store at one moment. The second machine has written
// since, and an unmeasured memory is kept under its legacy scope.
func TestCurateRetainsWhatItNeverMeasured(t *testing.T) {
	curated, keep := Curate(store.LegacyMemory{
		ID: "beacon-unseen", ProjectSlug: "beacon", Kind: "gotcha",
		Body: "flock is advisory. It only excludes other flock callers.", Source: "  a skill  ",
	})
	if !keep {
		t.Fatal("an unmeasured memory was dropped")
	}
	if curated.ProjectSlug != "beacon" {
		t.Errorf("unmeasured memory landed in %q, want its legacy scope beacon", curated.ProjectSlug)
	}
	if curated.Provenance == nil || *curated.Provenance != "a skill" {
		t.Errorf("provenance = %v, want the trimmed source", curated.Provenance)
	}
	if curated.Title == "" {
		t.Error("a retained memory carries no title")
	}
}

// An unscoped legacy memory has somewhere to go: global owns what names no
// subject.
func TestCurateScopesAnUnscopedMemoryGlobally(t *testing.T) {
	curated, keep := Curate(store.LegacyMemory{ID: "unseen-2", Body: "a note"})
	if !keep || curated.ProjectSlug != model.GlobalProjectSlug {
		t.Errorf("unscoped memory landed in %q, want global", curated.ProjectSlug)
	}
}

// The legacy kind column is dropped, so the kind has to survive as prose or it
// stops being searchable.
func TestCurateFoldsTheLegacyKindIntoTheBody(t *testing.T) {
	curated, _ := Curate(store.LegacyMemory{ID: "unseen-3", Body: "flock is advisory.", Kind: "gotcha"})
	if curated.Body != "gotcha: flock is advisory." {
		t.Errorf("body = %q, want the kind folded in", curated.Body)
	}
	already, _ := Curate(store.LegacyMemory{ID: "unseen-4", Body: "gotcha: flock is advisory.", Kind: "gotcha"})
	if already.Body != "gotcha: flock is advisory." {
		t.Errorf("body = %q, want the kind folded in once", already.Body)
	}
}

// v7 omission is not a tombstone, so carrying a deleted row across would
// resurrect it rather than migrate its deletion.
func TestCurateDropsAnUnmeasuredDeletedMemory(t *testing.T) {
	if _, keep := Curate(store.LegacyMemory{ID: "unseen-5", Body: "gone", Deleted: true}); keep {
		t.Error("a deleted unmeasured memory was resurrected as live")
	}
}

// Only the eight edges the curation named survive; every other legacy link
// pointed at a row it drops.
func TestCurateKeepsOnlyTheMeasuredSupersessions(t *testing.T) {
	retired := model.ID("br-et8")
	curated, keep := Curate(store.LegacyMemory{ID: "br-eyp", SupersededBy: &retired})
	if !keep || curated.SupersededBy == nil || *curated.SupersededBy != "br-et8" {
		t.Errorf("br-eyp supersession = %v, want br-et8", curated.SupersededBy)
	}

	elsewhere := model.ID("br-sgs")
	other, keep := Curate(store.LegacyMemory{ID: "br-1be", SupersededBy: &elsewhere})
	if !keep {
		t.Fatal("br-1be is retained and was dropped")
	}
	if other.SupersededBy != nil {
		t.Errorf("br-1be kept an unmeasured supersession %v", *other.SupersededBy)
	}
}

// A link into a dropped row is what makes the conversion refuse, and an
// unmeasured memory is not responsible for the curation's judgement.
func TestCurateSeversAnUnmeasuredLinkIntoADroppedMemory(t *testing.T) {
	dropped := model.ID("br-dnb")
	curated, keep := Curate(store.LegacyMemory{ID: "unseen-6", Body: "a note", SupersededBy: &dropped})
	if !keep {
		t.Fatal("an unmeasured memory was dropped for its link")
	}
	if curated.SupersededBy != nil {
		t.Errorf("kept a link into the dropped %v", *curated.SupersededBy)
	}

	live := model.ID("br-et8")
	linked, _ := Curate(store.LegacyMemory{ID: "unseen-7", Body: "a note", SupersededBy: &live})
	if linked.SupersededBy == nil || *linked.SupersededBy != "br-et8" {
		t.Errorf("severed a link into a retained memory: %v", linked.SupersededBy)
	}
}

// The curation is a census of 149 rows: 118 kept, 31 dropped, disjoint.
func TestCurationTablesAreTheMeasuredShape(t *testing.T) {
	if len(retainedMemories) != 118 {
		t.Errorf("retained %d memories, want 118", len(retainedMemories))
	}
	if len(droppedMemories) != 31 {
		t.Errorf("dropped %d memories, want 31", len(droppedMemories))
	}
	for id := range droppedMemories {
		if _, both := retainedMemories[id]; both {
			t.Errorf("%s is both retained and dropped", id)
		}
	}
	for retired, next := range retainedSupersessions {
		if _, kept := retainedMemories[retired]; !kept {
			t.Errorf("supersession source %s is not retained", retired)
		}
		if _, kept := retainedMemories[next]; !kept {
			t.Errorf("supersession target %s is not retained", next)
		}
	}
}
