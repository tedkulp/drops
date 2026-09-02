package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
)

// ErrFork reports a fetched snapshot that does not descend linearly from the
// head this store last accepted from its source replica: the source's history
// diverged, which duplicate replica keys make detectable rather than invisible.
var ErrFork = errors.New("replica history has forked")

// ImportResult reports what a pull observed and merged at the remote, without
// having committed anything local.
type ImportResult struct {
	// Head is the remote branch tip observed ("" when the remote is unborn).
	Head string
	// New is true when a snapshot was fetched and merged.
	New bool
	// Report is core's account of the merge; nil when nothing was fetched.
	Report *core.ImportReport
}

// fetchRemote fetches the remote branch tip, or reports the empty-remote state.
func (t *Transport) fetchRemote(ctx context.Context) (string, error) {
	fetched, err := t.git.Fetch(ctx, t.dir, t.remote, t.branch)
	if err == nil {
		return fetched.SHA, nil
	}
	var commandErr *gitx.CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode >= 0 && !commandErr.TimedOut &&
		(containsFold(commandErr.Stderr, "no matching refs") ||
			containsFold(commandErr.Stderr, "couldn't find remote ref")) {
		return "", nil
	}
	return "", fmt.Errorf("fetch %s: %w", t.remote, err)
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// importRemote merges the snapshot at head into the store, proving its lineage
// first. It never changes the destination's active replica key.
func (t *Transport) importRemote(ctx context.Context, head string) (ImportResult, error) {
	raw, err := t.git.ReadFile(ctx, t.dir, head, mirrorFileName)
	if err != nil {
		return ImportResult{}, fmt.Errorf("read remote mirror at %s: %w", head, err)
	}
	snapshot, err := mirror.Parse(raw)
	if err != nil {
		return ImportResult{}, fmt.Errorf("parse remote mirror at %s: %w", head, err)
	}

	myKey, err := t.activeReplicaKey()
	if err != nil {
		return ImportResult{}, err
	}
	source := authorReplica(snapshot, myKey)
	if source == "" {
		// The snapshot carries nothing authored by another replica: it is a
		// re-export of records this store already owns, so merge without
		// attributing or recording a head.
		report, err := t.core.Import(ctx, snapshot)
		if err != nil {
			return ImportResult{}, err
		}
		return ImportResult{Head: head, New: true, Report: &report}, nil
	}
	if err := t.proveLineage(ctx, snapshot, source); err != nil {
		return ImportResult{}, err
	}
	report, err := t.core.ImportWithHead(ctx, snapshot, model.ReplicaHead{Replica: source, SnapshotHead: head})
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Head: head, New: true, Report: &report}, nil
}

// proveLineage rejects a snapshot from a replica whose last accepted head is not
// named among its predecessors: a linear advance must descend from what this
// store already holds, and any other successor is a fork.
func (t *Transport) proveLineage(ctx context.Context, snapshot mirror.Snapshot, source model.ReplicaKey) error {
	heads, err := t.core.ReplicaHeads(ctx)
	if err != nil {
		return err
	}
	for _, head := range heads {
		if head.Replica != source {
			continue
		}
		for _, predecessor := range snapshot.Predecessors {
			if predecessor == head.SnapshotHead {
				return nil // linear advance from the accepted head
			}
		}
		return fmt.Errorf("%w: %s produced successors %q not descending from accepted head %q",
			ErrFork, source, snapshot.Predecessors, head.SnapshotHead)
	}
	return nil
}

// activeReplicaKey reads the sidecar key; a store with no sidecar has no active
// key and yields "" (unattributed).
func (t *Transport) activeReplicaKey() (model.ReplicaKey, error) {
	sidecar, err := LoadSidecar(t.dir)
	if err != nil {
		return "", err
	}
	if sidecar == nil {
		return "", nil
	}
	return sidecar.Replica, nil
}

// authorReplica returns the replica key that authored a snapshot's newest
// revision, excluding my key and the reserved-project keys, or "" when no such
// key exists (the snapshot carries nothing new).
func authorReplica(snapshot mirror.Snapshot, mine model.ReplicaKey) model.ReplicaKey {
	newest := int64(0)
	var source model.ReplicaKey
	for _, record := range snapshot.Records {
		replica, generation := recordReplica(record)
		if replica == "" || replica == mine || isReservedKey(replica) {
			continue
		}
		if generation > newest {
			newest = generation
			source = replica
		}
	}
	return source
}

func isReservedKey(key model.ReplicaKey) bool {
	return key == model.ReplicaKey(model.GlobalProjectKey) ||
		key == model.ReplicaKey(model.InboxProjectKey)
}

// recordReplica reports the authoring replica key and generation of one record.
func recordReplica(record mirror.Record) (model.ReplicaKey, int64) {
	switch value := record.Value.(type) {
	case model.Project:
		return value.Revision.Replica, value.Revision.Generation
	case model.RepositoryLocator:
		return value.Revision.Replica, value.Revision.Generation
	case model.Issue:
		return value.Revision.Replica, value.Revision.Generation
	case model.IssueParent:
		return value.Revision.Replica, value.Revision.Generation
	case model.Dependency:
		return value.Revision.Replica, value.Revision.Generation
	case model.Label:
		return value.Revision.Replica, value.Revision.Generation
	case model.Comment:
		rev := value.Revision()
		return rev.Replica, rev.Generation
	case model.Memory:
		return value.Revision.Replica, value.Revision.Generation
	case model.ReplicaSuccession:
		return value.NewReplica, 1
	default:
		return "", 0
	}
}
