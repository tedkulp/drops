package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

func openTransport(t *testing.T) (*Transport, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	opened, err := store.Open(t.Context(), dir+"/drops.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	key, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	rules := core.New(opened, key, nil, nil, nil)
	if err := rules.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	return New(rules, opened, Options{}), opened
}

func issueRecord(t *testing.T, key model.ReplicaKey, generation int64, id string) mirror.Record {
	t.Helper()
	now := model.NewTimestamp(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	record, err := mirror.NewRecord(model.Issue{
		ID:              model.ID(id),
		ProjectKey:      model.GlobalProjectKey,
		Title:           "x",
		Type:            model.TypeTask,
		Status:          model.StatusOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreationReplica: key,
		Revision:        model.Revision{Generation: generation, Replica: key},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestProveLineageRejectsAFork(t *testing.T) {
	transport, _ := openTransport(t)

	head := "feedc0de0000000000000000000000000000000"
	if _, err := transport.core.ImportWithHead(t.Context(), mirror.Snapshot{Records: []mirror.Record{}}, model.ReplicaHead{
		Replica: "aaaaaaaaaaaaaaaaaaaaaaaaae", SnapshotHead: head,
	}); err != nil {
		t.Fatalf("seed head: %v", err)
	}

	// A second snapshot from the same replica must descend from head; one that
	// descends from elsewhere is a fork.
	fork := mirror.Snapshot{Predecessors: []string{"deadbeef00000000000000000000000000000000"}}
	err := transport.proveLineage(t.Context(), fork, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	if !errors.Is(err, ErrFork) {
		t.Fatalf("proveLineage = %v, want ErrFork", err)
	}

	linear := mirror.Snapshot{Predecessors: []string{head}}
	if err := transport.proveLineage(t.Context(), linear, "aaaaaaaaaaaaaaaaaaaaaaaaae"); err != nil {
		t.Fatalf("linear proveLineage = %v, want nil", err)
	}
}

func TestAuthorReplicaSkipsMineAndReserved(t *testing.T) {
	mine := model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	other := model.ReplicaKey("bbbbbbbbbbbbbbbbbbbbbbbbbe")

	snapshot := mirror.Snapshot{Records: []mirror.Record{
		issueRecord(t, mine, 2, "aaaaa"),                                      // mine, skipped
		issueRecord(t, other, 1, "bbbbb"),                                     // other, candidate
		issueRecord(t, other, 3, "ccccc"),                                     // other, newer
		issueRecord(t, model.ReplicaKey(model.GlobalProjectKey), 99, "ddddd"), // reserved, skipped
	}}
	if got := authorReplica(snapshot, mine); got != other {
		t.Fatalf("authorReplica = %q, want %q", got, other)
	}
}
