package core_test

import (
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// otherReplica is a second machine's key, distinct from the one openCore uses.
func otherReplica(t *testing.T) model.ReplicaKey {
	t.Helper()
	key, err := model.ParseReplicaKey("ccccccccccccccccccccccccce")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	return key
}

// importOne merges exactly one hand-built record.
func importOne(t *testing.T, rules *core.Core, value any) (core.ImportReport, error) {
	t.Helper()
	record, err := mirror.NewRecord(value)
	if err != nil {
		t.Fatalf("wrap record: %v", err)
	}
	return rules.Import(t.Context(), mirror.Snapshot{Records: []mirror.Record{record}})
}

// seeded builds a target holding one Project, one Issue and one Comment, all
// originating on this replica, ready for an incoming record to collide with.
func seeded(t *testing.T) (*core.Core, *store.Store, model.Project, model.Issue, model.Comment) {
	t.Helper()
	rules, opened := openCoreWithIDs(t, "shared-id")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "held", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	comment, err := rules.AddComment(t.Context(), issue.ID, "ted", "a remark that is already here")
	if err != nil {
		t.Fatalf("add Comment: %v", err)
	}
	return rules, opened, project, issue, comment
}

// The clause (dw32p.5): a Project key that already exists here but claims a
// different creation origin is an identity collision, and a collision refuses
// the WHOLE import rather than merging the imposter.
//
// A Project has no ID reservation to check — unlike an Issue or a Memory, its
// key is not in id_owners — so this branch is the only thing standing between
// two independently-minted Projects that happen to share a key and a silently
// merged one.
func TestImportRefusesAProjectWhoseOriginDiffers(t *testing.T) {
	rules, _, project, _, _ := seeded(t)

	imposter := project
	imposter.CreationReplica = otherReplica(t)
	imposter.Slug = "imposter"
	imposter.Revision = model.Revision{Generation: 2, Replica: otherReplica(t)}

	report, err := importOne(t, rules, imposter)
	var collision model.IdentityCollision
	if !errors.As(err, &collision) {
		t.Fatalf("import = %#v, %v, want an IdentityCollision", report, err)
	}
	if !errors.Is(err, model.ErrConflict) {
		t.Errorf("collision is not an ErrConflict: %v", err)
	}
	if collision.Record.Kind != model.RecordProject || collision.Record.Key != string(project.Key) {
		t.Errorf("collision names %#v, want the Project %s", collision.Record, project.Key)
	}
	if collision.CurrentOrigin == collision.IncomingOrigin {
		t.Errorf("collision reports one origin twice: %#v", collision)
	}
	unchanged, err := rules.Project(t.Context(), project.Key)
	if err != nil {
		t.Fatalf("read back Project: %v", err)
	}
	if unchanged.Slug != project.Slug {
		t.Errorf("Project slug = %q, want %q — the refused import committed anyway", unchanged.Slug, project.Slug)
	}
}

// The clause (dw32p.5): the same refusal covers a Comment, the other record
// whose identity is not reserved in id_owners. A Comment id is derived from its
// Issue, so two machines commenting on one Issue can mint the same id.
func TestImportRefusesACommentWhoseOriginDiffers(t *testing.T) {
	rules, _, _, _, comment := seeded(t)

	imposter := comment
	imposter.CreationReplica = otherReplica(t)
	imposter.Body = "a different remark wearing the same id"

	report, err := importOne(t, rules, imposter)
	var collision model.IdentityCollision
	if !errors.As(err, &collision) {
		t.Fatalf("import = %#v, %v, want an IdentityCollision", report, err)
	}
	if collision.Record.Kind != model.RecordComment || collision.Record.Key != string(comment.ID) {
		t.Errorf("collision names %#v, want the Comment %s", collision.Record, comment.ID)
	}
	held, err := rules.IssueComments(t.Context(), comment.IssueID)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	if len(held) != 1 || held[0].Body != comment.Body {
		t.Fatalf("thread = %#v, want the original Comment untouched", held)
	}
}

// The clause (dw32p.7): an ID is owned by ONE kind permanently. An incoming
// Memory carrying an id this store reserved for an Issue collides even though
// both originate on the same replica — the origins match, so only the kind
// half of the check can catch it.
func TestImportRefusesAnIDReservedForAnotherKind(t *testing.T) {
	rules, _, project, issue, _ := seeded(t)
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}

	trespasser := model.Memory{
		ID: issue.ID, ProjectKey: project.Key, Title: "not an Issue",
		Body:      "a Memory wearing an Issue's reserved id",
		CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt,
		CreationReplica: replica, Revision: model.Revision{Generation: 1, Replica: replica},
	}

	report, err := importOne(t, rules, trespasser)
	var collision model.IdentityCollision
	if !errors.As(err, &collision) {
		t.Fatalf("import = %#v, %v, want an IdentityCollision", report, err)
	}
	if collision.Record.Kind != model.RecordMemory || collision.Record.Key != string(issue.ID) {
		t.Errorf("collision names %#v, want the Memory %s", collision.Record, issue.ID)
	}
	if _, err := rules.Memory(t.Context(), issue.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("the refused Memory was written anyway: %v", err)
	}
	stillAnIssue, err := rules.Issue(t.Context(), issue.ID)
	if err != nil || stillAnIssue.Title != issue.Title {
		t.Fatalf("Issue = %#v, %v, want it untouched", stillAnIssue, err)
	}
}

// The clause: an incoming record whose id this store has never reserved is not
// a collision — the check must not refuse every unknown id, which would make
// first sync impossible.
func TestImportAcceptsAnUnreservedID(t *testing.T) {
	rules, _, project, _, _ := seeded(t)
	replica := otherReplica(t)

	arriving := model.Memory{
		ID: "mem-elsewhere", ProjectKey: project.Key, Title: "from the other machine",
		Body:      "a Memory this store has never seen",
		CreatedAt: model.NewTimestamp(fixedNow), UpdatedAt: model.NewTimestamp(fixedNow),
		CreationReplica: replica, Revision: model.Revision{Generation: 1, Replica: replica},
	}
	report, err := importOne(t, rules, arriving)
	if err != nil {
		t.Fatalf("import an unreserved id: %v", err)
	}
	if report.Applied != 1 {
		t.Fatalf("report = %#v, want one applied record", report)
	}
	landed, err := rules.Memory(t.Context(), "mem-elsewhere")
	if err != nil || landed.CreationReplica != replica {
		t.Fatalf("imported Memory = %#v, %v", landed, err)
	}
}
