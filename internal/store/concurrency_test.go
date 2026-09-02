package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// TestConcurrentWritersQueueRatherThanCollide exercises the DSN's
// _txlock=immediate. Go's default deferred BEGIN lets several transactions open
// together and then race to upgrade to the write lock, and that upgrade
// collision returns SQLITE_BUSY immediately instead of waiting out busy_timeout.
// Two handles on one file are used because a single pool would serialize some of
// this itself.
func TestConcurrentWritersQueueRatherThanCollide(t *testing.T) {
	first := newStore(t)
	second, err := store.Open(t.Context(), first.Path())
	if err != nil {
		t.Fatalf("open a second handle: %v", err)
	}
	defer second.Close()

	replica := newReplicaKey(t)
	project := seedProject(t, first, replica, "drops")

	const writers = 8
	var (
		wait   sync.WaitGroup
		mutex  sync.Mutex
		errors []error
	)
	for writer := range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			handle := first
			if writer%2 == 1 {
				handle = second
			}
			id := model.ID(fmt.Sprintf("issue-%d", writer))
			err := handle.WithTx(t.Context(), func(ctx context.Context, tx *store.Tx) error {
				// Read before writing. A deferred BEGIN takes only a read lock
				// here and has to upgrade for the write, which is the collision
				// _txlock=immediate exists to prevent; a transaction that writes
				// first never exercises it.
				if _, err := tx.State(ctx); err != nil {
					return err
				}
				if _, err := tx.Issues(ctx, store.IssueFilter{}); err != nil {
					return err
				}
				if err := tx.PutIDOwner(ctx, model.IDOwner{
					ID: id, Kind: model.OwnerIssue, CreationReplica: replica,
				}); err != nil {
					return err
				}
				revision, err := model.InitialRevision(replica)
				if err != nil {
					return err
				}
				return tx.PutIssue(ctx, model.Issue{
					ID: id, ProjectKey: project.Key, Title: string(id),
					Type: model.TypeTask, Status: model.StatusOpen, Priority: 2,
					CreatedAt: stampCreated, UpdatedAt: stampUpdated,
					CreationReplica: replica, Revision: revision,
				})
			})
			if err != nil {
				mutex.Lock()
				errors = append(errors, err)
				mutex.Unlock()
			}
		}()
	}
	wait.Wait()

	if len(errors) > 0 {
		t.Fatalf("%d of %d concurrent writers failed; first: %v", len(errors), writers, errors[0])
	}
	listed, err := first.Issues(t.Context(), store.IssueFilter{})
	if err != nil {
		t.Fatalf("list Issues: %v", err)
	}
	if len(listed) != writers {
		t.Errorf("wrote %d Issues, want %d", len(listed), writers)
	}
	// The counter is bumped inside each transaction, so it counts commits
	// exactly rather than losing one to a lost update.
	if seq := stateOf(t, first).WriteSeq; seq != int64(writers)+1 {
		t.Errorf("write_seq = %d, want %d (the Project plus one per Issue)", seq, writers+1)
	}
}
