package core_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
)

// The clause (dw32p.7): the two reserved Projects are created with FIXED keys,
// slugs and origins, so two machines that each bootstrap independently agree on
// them without ever syncing. Bootstrap is idempotent for the same reason.
func TestBootstrapCreatesConvergentReservedProjects(t *testing.T) {
	first, _ := openCore(t)
	if err := first.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	global, err := first.Project(t.Context(), model.GlobalProjectKey)
	if err != nil {
		t.Fatalf("read global: %v", err)
	}
	inbox, err := first.Project(t.Context(), model.InboxProjectKey)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if global.Slug != "global" || inbox.Slug != "inbox" {
		t.Fatalf("slugs = %q, %q, want global and inbox", global.Slug, inbox.Slug)
	}
	if global.ArchivedAt != nil || inbox.ArchivedAt != nil {
		t.Fatal("a reserved Project arrived archived")
	}

	// Bootstrapping again changes nothing, so a second session at startup
	// does not bump a revision the other machine would then have to merge.
	if err := first.Bootstrap(t.Context()); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	again, err := first.Project(t.Context(), model.GlobalProjectKey)
	if err != nil {
		t.Fatalf("re-read global: %v", err)
	}
	if again.Revision != global.Revision || again.UpdatedAt != global.UpdatedAt {
		t.Fatalf("second bootstrap rewrote global: %#v, want %#v", again, global)
	}

	// A second, independent store agrees byte for byte, which is the whole
	// point of fixing the key, the origin and the timestamp. Its clock reads a
	// different hour on purpose: a reserved Project stamped from the wall clock
	// would differ between the two machines and then merge as a conflict.
	_, otherStore := openCore(t)
	replica, err := model.ParseReplicaKey("bbbbbbbbbbbbbbbbbbbbbbbbbe")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	second := core.New(otherStore, replica, fixedClock{now: fixedNow.Add(37 * time.Hour)}, nil, nil)
	if err := second.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap second store: %v", err)
	}
	elsewhere, err := second.Project(t.Context(), model.GlobalProjectKey)
	if err != nil {
		t.Fatalf("read global on second store: %v", err)
	}
	if elsewhere != global {
		t.Fatalf("independent bootstraps disagree:\n %#v\n %#v", elsewhere, global)
	}
}

// The clause: a reserved Project that has been tampered with is a conflict
// rather than something Bootstrap silently repairs — the key is permanent, so
// a malformed one means two different things claim it.
func TestBootstrapRefusesAMalformedReservedProject(t *testing.T) {
	rules, opened := openCore(t)
	if err := rules.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// Every rules-level path already refuses to damage a reserved Project —
	// rename and archive both say so — so the damage is done underneath them,
	// which is the only way this state can arise: another build, or a store
	// edited by hand.
	if _, err := opened.DB().ExecContext(t.Context(),
		"UPDATE projects SET slug = 'not-global' WHERE project_key = ?", string(model.GlobalProjectKey),
	); err != nil {
		t.Fatalf("corrupt the reserved Project: %v", err)
	}

	if err := rules.Bootstrap(t.Context()); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("bootstrap over a malformed reserved Project = %v, want ErrConflict", err)
	}
}

// The clause (dw32p.10): an import and the source head it came from commit
// TOGETHER, so a crash cannot leave a store holding records it will never
// import again because it already recorded having seen them.
func TestImportWithHeadCommitsRecordsAndHeadAsOneUnit(t *testing.T) {
	source, _ := openCoreWithIDs(t, "shared")
	project, err := source.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create source Project: %v", err)
	}
	if _, err := source.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "from source", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create source Issue: %v", err)
	}
	snapshot, err := source.Export(t.Context(), nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	target, _ := openCore(t)
	head := model.ReplicaHead{Replica: otherReplica(t), SnapshotHead: "9f3c1a7"}
	if _, err := target.ImportWithHead(t.Context(), snapshot, head); err != nil {
		t.Fatalf("import with head: %v", err)
	}
	heads, err := target.ReplicaHeads(t.Context())
	if err != nil || len(heads) != 1 || heads[0] != head {
		t.Fatalf("heads = %#v, %v, want %#v", heads, err, head)
	}
	if _, err := target.Issue(t.Context(), "shared"); err != nil {
		t.Fatalf("imported Issue: %v", err)
	}
}

// The clause: a head that does not validate is refused BEFORE the snapshot is
// merged, so a caller cannot import records against a head nobody can name.
func TestImportWithHeadRefusesAnInvalidHeadBeforeImporting(t *testing.T) {
	source, _ := openCoreWithIDs(t, "shared")
	project, err := source.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create source Project: %v", err)
	}
	if _, err := source.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "from source", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create source Issue: %v", err)
	}
	snapshot, err := source.Export(t.Context(), nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	target, _ := openCore(t)
	empty := model.ReplicaHead{Replica: otherReplica(t), SnapshotHead: ""}
	if _, err := target.ImportWithHead(t.Context(), snapshot, empty); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("import with an empty head = %v, want ErrInvalid", err)
	}
	if _, err := target.Issue(t.Context(), "shared"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("records landed under a refused head: %v", err)
	}
	heads, err := target.ReplicaHeads(t.Context())
	if err != nil || len(heads) != 0 {
		t.Fatalf("heads = %#v, %v, want none", heads, err)
	}
}

// The clause: an export captures the write sequence it read, and MarkExported
// records exactly that one — not the sequence at the time of the commit, which
// a concurrent write may already have moved past.
func TestMarkExportedRecordsTheCapturedWriteSequence(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "first", "later")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "first", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create Issue: %v", err)
	}

	batch, err := rules.PrepareExport(t.Context(), nil)
	if err != nil {
		t.Fatalf("prepare export: %v", err)
	}
	// A write that lands after the snapshot was read but before it was marked.
	if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "later", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create later Issue: %v", err)
	}

	if err := rules.MarkExported(t.Context(), batch.WriteSeq, "9f3c1a7"); err != nil {
		t.Fatalf("mark exported: %v", err)
	}
	state, err := rules.State(t.Context())
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.ExportedWriteSeq != batch.WriteSeq {
		t.Fatalf("exported seq = %d, want the captured %d", state.ExportedWriteSeq, batch.WriteSeq)
	}
	if state.LastExportHead != "9f3c1a7" {
		t.Fatalf("last export head = %q", state.LastExportHead)
	}
	if state.Exported() {
		t.Fatal("the store reports itself fully exported, but a write landed after the snapshot")
	}
}

// The clause (dw32p.10): a Replica succession chain is one-to-one and ACYCLIC.
// A succession that would close a loop is refused with nothing committed —
// a cycle would make "which key is current" unanswerable.
func TestRecordReplicaSuccessionRefusesACycle(t *testing.T) {
	rules, _ := openCore(t)
	first, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	second := otherReplica(t)

	if _, err := rules.RecordReplicaSuccession(t.Context(), first, second); err != nil {
		t.Fatalf("record succession: %v", err)
	}
	successions, err := rules.ReplicaSuccessions(t.Context())
	if err != nil || len(successions) != 1 {
		t.Fatalf("successions = %#v, %v", successions, err)
	}

	if _, err := rules.RecordReplicaSuccession(t.Context(), second, first); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("cyclic succession = %v, want ErrInvalid", err)
	}
	after, err := rules.ReplicaSuccessions(t.Context())
	if err != nil || len(after) != 1 {
		t.Fatalf("successions after the refusal = %#v, %v, want the one original", after, err)
	}
}

// The clause: a snapshot carrying the same record twice is refused. Two rows
// for one key would make the merge order decide the winner, which is exactly
// the non-determinism the whole import design exists to avoid.
func TestImportRefusesADuplicateRecord(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "shared")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	record, err := mirror.NewRecord(project)
	if err != nil {
		t.Fatalf("wrap Project: %v", err)
	}

	target, _ := openCore(t)
	_, err = target.Import(t.Context(), mirror.Snapshot{Records: []mirror.Record{record, record}})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("duplicate record error = %v, want ErrInvalid", err)
	}
	if _, err := target.Project(t.Context(), project.Key); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("the refused snapshot committed anyway: %v", err)
	}
}

// The clause (dw32p.7): the reserved Projects arrive over the mirror like any
// other record, so an incoming one that is not the reserved shape — wrong slug,
// or archived — is refused rather than merged over the local pair.
func TestImportRefusesAMalformedReservedProject(t *testing.T) {
	rules, _ := openCore(t)
	if err := rules.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	global, err := rules.Project(t.Context(), model.GlobalProjectKey)
	if err != nil {
		t.Fatalf("read global: %v", err)
	}

	renamed := global
	renamed.Slug = "not-global"
	renamed.Revision = model.Revision{Generation: 2, Replica: otherReplica(t)}
	if _, err := importOne(t, rules, renamed); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("renamed reserved Project = %v, want ErrInvalid", err)
	}

	// And the other direction: an ordinary Project may not claim a reserved slug.
	stamp := model.NewTimestamp(fixedNow)
	key, err := model.NewProjectKey()
	if err != nil {
		t.Fatalf("mint key: %v", err)
	}
	pretender := model.Project{
		Key: key, Slug: "inbox", CreatedAt: stamp, UpdatedAt: stamp,
		CreationReplica: otherReplica(t),
		Revision:        model.Revision{Generation: 1, Replica: otherReplica(t)},
	}
	if _, err := importOne(t, rules, pretender); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("Project claiming a reserved slug = %v, want ErrInvalid", err)
	}

	unchanged, err := rules.Project(t.Context(), model.GlobalProjectKey)
	if err != nil || unchanged.Slug != "global" {
		t.Fatalf("global = %#v, %v, want it untouched", unchanged, err)
	}
}
