package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/model"
)

// maxPushRetries bounds how many times a rejected push re-pulls and re-merges
// before giving up. Each retry folds another concurrent remote advance into a
// fresh merge, so the bound also bounds merge depth.
const maxPushRetries = 4

// Result is one sync's account.
type Result struct {
	Exported  bool
	Imported  bool
	Head      string
	Applied   int
	Ignored   int
	Conflicts []model.MergeConflict
	Unlocked  bool
}

// Sync runs one transport cycle: pull the remote, import anything new, then
// export and push any local writes, resolving divergences as deterministic merge
// commits. It never changes the destination's active replica key.
func (t *Transport) Sync(ctx context.Context) (Result, error) {
	release, locked, err := t.withLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	result := Result{Unlocked: !locked}

	if err := t.ensureRepo(ctx); err != nil {
		return result, err
	}
	if _, err := t.ensureSidecar(); err != nil {
		return result, err
	}

	state0, err := t.store.State(ctx)
	if err != nil {
		return result, err
	}
	localDirty := !state0.Exported()

	myHead, err := t.git.Head(ctx, t.dir)
	if err != nil {
		return result, err
	}
	remoteHead, err := t.fetchRemote(ctx)
	if err != nil {
		return result, err
	}

	imported := false
	if remoteHead != "" && remoteHead != myHead {
		if myHead == "" {
			if err := t.git.Adopt(ctx, t.dir, t.remote, t.branch); err != nil {
				return result, err
			}
			myHead = remoteHead
		}
		imp, err := t.importRemote(ctx, remoteHead)
		if err != nil {
			return result, err
		}
		imported = true
		result.Imported = true
		result.Head = remoteHead
		if imp.Report != nil {
			result.Applied = imp.Report.Applied
			result.Ignored = imp.Report.Ignored
			result.Conflicts = imp.Report.Conflicts
		}
	}

	if !localDirty {
		if imported {
			// Pure pull: fast-forward the branch to the remote tip and note that
			// our state now equals it, so the next local write builds on the
			// shared history.
			if err := t.git.FastForward(ctx, t.dir, t.branch, remoteHead); err != nil {
				return result, err
			}
			state, err := t.store.State(ctx)
			if err != nil {
				return result, err
			}
			if err := t.recordExport(ctx, state.WriteSeq, remoteHead); err != nil {
				return result, err
			}
			result.Head = remoteHead
		}
		return result, nil
	}

	return t.exportAndPush(ctx, myHead, remoteHead, imported, result)
}

// exportAndPush exports the current (possibly merged) state, commits it, pushes,
// and on a rejected non-fast-forward re-pulls and re-merges up to the bound.
func (t *Transport) exportAndPush(ctx context.Context, myHead, remoteHead string, imported bool, result Result) (Result, error) {
	sidecar, err := t.ensureSidecar()
	if err != nil {
		return result, err
	}
	if err := t.checkCoherence(ctx, sidecar); err != nil {
		return result, err
	}

	var writeSeq int64
	var head string
	for retry := 0; ; retry++ {
		preds := []string{}
		if myHead != "" {
			preds = append(preds, myHead)
		}
		if imported && remoteHead != "" && remoteHead != myHead {
			preds = append(preds, remoteHead)
		}

		batch, raw, err := t.prepareExport(ctx, preds)
		if err != nil {
			return result, err
		}
		writeSeq = batch.WriteSeq
		if err := t.writeMirror(raw); err != nil {
			return result, err
		}

		if imported && remoteHead != "" && remoteHead != myHead {
			commit, err := t.git.MergeCommit(ctx, t.dir, commitMessage(batch),
				[]string{mirrorFileName}, remoteHead)
			if err != nil {
				return result, err
			}
			head = commit.SHA
		} else {
			commit, err := t.git.Commit(ctx, t.dir, commitMessage(batch), []string{mirrorFileName})
			if err != nil {
				return result, err
			}
			head = commit.SHA
		}

		err = t.git.Push(ctx, t.dir, t.remote, t.branch)
		if err == nil {
			break
		}
		if !isPushRejected(err) || retry >= maxPushRetries {
			return result, fmt.Errorf("push %s: %w", t.remote, err)
		}
		// A concurrent advance landed behind us: pull and fold it in, then retry.
		nextHead, ferr := t.fetchRemote(ctx)
		if ferr != nil {
			return result, ferr
		}
		if nextHead == "" || nextHead == remoteHead {
			return result, fmt.Errorf("push %s: %w", t.remote, err)
		}
		if _, ierr := t.importRemote(ctx, nextHead); ierr != nil {
			return result, ierr
		}
		remoteHead = nextHead
		imported = true
	}

	if head == "" {
		head = myHead
		if head == "" {
			head = remoteHead
		}
	}
	if err := t.recordExport(ctx, writeSeq, head); err != nil {
		return result, err
	}
	result.Exported = true
	result.Head = head
	return result, nil
}

func commitMessage(batch core.ExportBatch) string {
	return fmt.Sprintf("drops sync: %d records", len(batch.Snapshot.Records))
}

func isPushRejected(err error) bool {
	var commandErr *gitx.CommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	message := strings.ToLower(commandErr.Stderr)
	return strings.Contains(message, "non-fast-forward") ||
		strings.Contains(message, "rejected") ||
		strings.Contains(message, "fetch first")
}
