package sync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
)

const (
	keyMine  = model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	keyOther = model.ReplicaKey("bbbbbbbbbbbbbbbbbbbbbbbbbe")
	keyThird = model.ReplicaKey("cccccccccccccccccccccccce")
)

func TestContainsFoldIgnoresCase(t *testing.T) {
	if !containsFold("fatal: Couldn't Find Remote Ref refs/heads/main", "couldn't find remote ref") {
		t.Error("containsFold missed a match differing only in case")
	}
	if containsFold("fatal: repository not found", "couldn't find remote ref") {
		t.Error("containsFold matched an unrelated message")
	}
}

// bareRemote makes an empty bare repository and returns its path.
func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "--bare", "-q", "-b", "main", remote)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return remote
}

// TestFetchRemoteReadsAnUnbornRemoteAsEmpty is the first-sync case: the shared
// remote exists but carries no branch yet, which is not a failure.
func TestFetchRemoteReadsAnUnbornRemoteAsEmpty(t *testing.T) {
	remote := bareRemote(t)
	transport, _ := openTransportWith(t, Options{URL: remote})
	if err := transport.ensureRepo(t.Context()); err != nil {
		t.Fatalf("ensureRepo: %v", err)
	}

	head, err := transport.fetchRemote(t.Context())
	if err != nil {
		t.Fatalf("fetchRemote against an unborn remote = %v, want nil", err)
	}
	if head != "" {
		t.Fatalf("fetchRemote = %q, want the empty-remote head", head)
	}
}

// TestFetchRemotePropagatesARealFailure is the other side: a remote that cannot
// be reached at all must not read as "nothing to import", because the sync
// would then export over it.
func TestFetchRemotePropagatesARealFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-a-repository.git")
	transport, _ := openTransportWith(t, Options{URL: missing})
	if err := transport.ensureRepo(t.Context()); err != nil {
		t.Fatalf("ensureRepo: %v", err)
	}

	if head, err := transport.fetchRemote(t.Context()); err == nil {
		t.Fatalf("fetchRemote against a missing remote = (%q, nil), want an error", head)
	}
}

// installFetchShim puts a `git` on PATH that answers every invocation with the
// given stderr and exit status, or hangs when hang is set. Git's own wording for
// an unborn branch is pinned by TestFetchRemoteReadsAnUnbornRemoteAsEmpty; this
// is how the classifier is held to the wordings that test cannot reach.
func installFetchShim(t *testing.T, stderr string, exit int, hang bool) {
	t.Helper()
	shim := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(stderr) + " >&2\n"
	if hang {
		script += "sleep 30\n"
	}
	script += "exit " + itoa(exit) + "\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func itoa(n int) string { return strconv.Itoa(n) }

// TestFetchRemoteClassifiesWhatGitSaid: only a completed git run reporting an
// unborn branch reads as an empty remote. Everything else — an unreachable
// remote, or a stall that printed the same wording before it was killed — has to
// stay an error, because reading it as "nothing to import" lets the very next
// step push over whatever the remote actually holds.
func TestFetchRemoteClassifiesWhatGitSaid(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		exit    int
		hang    bool
		wantErr bool
	}{
		{name: "couldn't find remote ref", stderr: "fatal: couldn't find remote ref refs/heads/main", exit: 128},
		{name: "no matching refs", stderr: "fatal: no matching refs in remote", exit: 128},
		{name: "unrelated failure", stderr: "fatal: repository not found", exit: 128, wantErr: true},
		{name: "timed out mid-message", stderr: "fatal: couldn't find remote ref refs/heads/main", hang: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, _ := openTransportWith(t, Options{
				Git: gitx.Client{NetworkTimeout: 200 * time.Millisecond, WaitDelay: 50 * time.Millisecond},
			})
			installFetchShim(t, tt.stderr, tt.exit, tt.hang)

			head, err := transport.fetchRemote(t.Context())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("fetchRemote = (%q, nil), want an error", head)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchRemote = %v, want the empty-remote reading", err)
			}
			if head != "" {
				t.Fatalf("fetchRemote = %q, want an empty head", head)
			}
		})
	}
}

// TestActiveReplicaKeyIsEmptyWithoutASidecar covers the importable-without-a-key
// case: a store that has never authored locally still merges.
func TestActiveReplicaKeyIsEmptyWithoutASidecar(t *testing.T) {
	transport, _ := openTransport(t)

	key, err := transport.activeReplicaKey()
	if err != nil {
		t.Fatalf("activeReplicaKey: %v", err)
	}
	if key != "" {
		t.Fatalf("activeReplicaKey = %q, want empty for a store with no sidecar", key)
	}

	minted, err := MintSidecar(transport.dir)
	if err != nil {
		t.Fatalf("MintSidecar: %v", err)
	}
	if key, err = transport.activeReplicaKey(); err != nil || key != minted.Replica {
		t.Fatalf("activeReplicaKey = (%q, %v), want the minted key %q", key, err, minted.Replica)
	}
}

// TestAuthorReplicaTakesTheNewestGeneration puts the newest revision on the
// *earlier* foreign record, so a comparison that keeps the first candidate it
// sees picks the wrong replica rather than the same one twice.
func TestAuthorReplicaTakesTheNewestGeneration(t *testing.T) {
	snapshot := mirror.Snapshot{Records: []mirror.Record{
		issueRecord(t, keyThird, 7, "aaaaa"),
		issueRecord(t, keyOther, 2, "bbbbb"),
	}}
	if got := authorReplica(snapshot, keyMine); got != keyThird {
		t.Fatalf("authorReplica = %q, want the newest generation's replica %q", got, keyThird)
	}

	reversed := mirror.Snapshot{Records: []mirror.Record{
		issueRecord(t, keyOther, 2, "bbbbb"),
		issueRecord(t, keyThird, 7, "aaaaa"),
	}}
	if got := authorReplica(reversed, keyMine); got != keyThird {
		t.Fatalf("authorReplica on the reversed snapshot = %q, want %q", got, keyThird)
	}
}

// TestAuthorReplicaIsEmptyWhenNothingIsForeign is the re-export case: a
// snapshot carrying only my own records has no source to attribute a head to.
func TestAuthorReplicaIsEmptyWhenNothingIsForeign(t *testing.T) {
	snapshot := mirror.Snapshot{Records: []mirror.Record{
		issueRecord(t, keyMine, 9, "aaaaa"),
		issueRecord(t, model.ReplicaKey(model.InboxProjectKey), 99, "bbbbb"),
	}}
	if got := authorReplica(snapshot, keyMine); got != "" {
		t.Fatalf("authorReplica = %q, want empty when nothing foreign is present", got)
	}
}

// TestRecordReplicaReadsEveryReplicatedKind: authorship is read off whichever
// record carries the newest revision, so every kind the mirror can carry has to
// answer. A kind that silently reports no replica makes a real fork invisible.
func TestRecordReplicaReadsEveryReplicatedKind(t *testing.T) {
	now := model.NewTimestamp(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	revision := model.Revision{Generation: 4, Replica: keyOther}

	tests := []struct {
		kind       model.RecordKind
		value      any
		generation int64
	}{
		{model.RecordProject, model.Project{Key: "p", Slug: "beacon", Revision: revision}, 4},
		{model.RecordRepositoryLocator, model.RepositoryLocator{ProjectKey: "p", Locator: "u", Revision: revision}, 4},
		{model.RecordIssue, model.Issue{ID: "aaaaa", Revision: revision}, 4},
		{model.RecordIssueParent, model.IssueParent{ChildID: "aaaaa", ParentID: "bbbbb", Revision: revision}, 4},
		{model.RecordDependency, model.Dependency{FromID: "aaaaa", ToID: "bbbbb", Revision: revision}, 4},
		{model.RecordLabel, model.Label{IssueID: "aaaaa", Name: "x", Revision: revision}, 4},
		{model.RecordComment, model.Comment{ID: "aaaaa:0", IssueID: "aaaaa", CreatedAt: now, CreationReplica: keyOther}, 1},
		{model.RecordMemory, model.Memory{ID: "mem-a", Revision: revision}, 4},
		{model.RecordReplicaSuccession, model.ReplicaSuccession{OldReplica: keyMine, NewReplica: keyOther, RekeyedAt: now}, 1},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			record, err := mirror.NewRecord(tt.value)
			if err != nil {
				t.Fatalf("NewRecord: %v", err)
			}
			if record.Kind != tt.kind {
				t.Fatalf("record kind = %q, want %q", record.Kind, tt.kind)
			}
			replica, generation := recordReplica(record)
			if replica != keyOther {
				t.Errorf("recordReplica replica = %q, want %q", replica, keyOther)
			}
			if generation != tt.generation {
				t.Errorf("recordReplica generation = %d, want %d", generation, tt.generation)
			}
		})
	}

	if replica, generation := recordReplica(mirror.Record{Kind: "unknown"}); replica != "" || generation != 0 {
		t.Errorf("recordReplica on an unknown kind = (%q, %d), want (\"\", 0)", replica, generation)
	}
}

// TestProveLineageAcceptsAFirstSnapshotFromAnUnknownReplica: a replica this
// store has never accepted from has no head to descend from, so its first
// snapshot is not a fork.
func TestProveLineageAcceptsAFirstSnapshotFromAnUnknownReplica(t *testing.T) {
	transport, _ := openTransport(t)

	if _, err := transport.core.ImportWithHead(t.Context(), mirror.Snapshot{}, model.ReplicaHead{
		Replica: keyOther, SnapshotHead: "feedc0de0000000000000000000000000000000",
	}); err != nil {
		t.Fatalf("seed head: %v", err)
	}

	first := mirror.Snapshot{Predecessors: []string{"deadbeef00000000000000000000000000000000"}}
	if err := transport.proveLineage(t.Context(), first, keyThird); err != nil {
		t.Fatalf("proveLineage for an unseen replica = %v, want nil", err)
	}
	if err := transport.proveLineage(t.Context(), first, keyOther); !errors.Is(err, ErrFork) {
		t.Fatalf("proveLineage for the seeded replica = %v, want ErrFork", err)
	}
}

// TestImportRemoteMergesASelfAuthoredSnapshotWithoutAttributingAHead is the
// re-import case a restored installation hits: the snapshot carries nothing but
// this store's own records, so there is no source replica to prove a lineage
// for and no head to record against one.
func TestImportRemoteMergesASelfAuthoredSnapshotWithoutAttributingAHead(t *testing.T) {
	transport, _ := openTransportWith(t, Options{URL: bareRemote(t)})
	// The sidecar key and core's authoring key are the same installation, which
	// is what makes every record in the export "mine".
	if err := (Sidecar{Replica: keyMine}).Save(transport.dir); err != nil {
		t.Fatal(err)
	}
	if err := transport.ensureRepo(t.Context()); err != nil {
		t.Fatal(err)
	}

	batch, raw, err := transport.prepareExport(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.writeMirror(raw); err != nil {
		t.Fatal(err)
	}
	commit, err := transport.git.Commit(t.Context(), transport.dir, commitMessage(batch), []string{mirrorFileName})
	if err != nil {
		t.Fatal(err)
	}

	result, err := transport.importRemote(t.Context(), commit.SHA)
	if err != nil {
		t.Fatalf("importRemote of a self-authored snapshot = %v, want a merge", err)
	}
	if !result.New || result.Head != commit.SHA || result.Report == nil {
		t.Fatalf("importRemote = %+v, want the merge reported at %s", result, commit.SHA)
	}

	heads, err := transport.core.ReplicaHeads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, head := range heads {
		if head.SnapshotHead == commit.SHA {
			t.Fatalf("importRemote recorded head %s against replica %s, want no attribution",
				head.SnapshotHead, head.Replica)
		}
	}
}

// commitSnapshot writes a snapshot as the mirror and commits it, returning the
// commit that a peer's fetch would name.
func commitSnapshot(t *testing.T, transport *Transport, snapshot mirror.Snapshot) string {
	t.Helper()
	raw, err := mirror.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.writeMirror(raw); err != nil {
		t.Fatal(err)
	}
	commit, err := transport.git.Commit(t.Context(), transport.dir, "peer snapshot", []string{mirrorFileName})
	if err != nil {
		t.Fatal(err)
	}
	return commit.SHA
}

// TestImportRemoteRefusesAForkBeforeMerging: the lineage proof runs before the
// merge, so a forked snapshot leaves the store exactly as it was. Proving it
// afterwards would be no proof at all.
func TestImportRemoteRefusesAForkBeforeMerging(t *testing.T) {
	transport, _ := openTransportWith(t, Options{URL: bareRemote(t)})
	if err := (Sidecar{Replica: keyMine}).Save(transport.dir); err != nil {
		t.Fatal(err)
	}
	if err := transport.ensureRepo(t.Context()); err != nil {
		t.Fatal(err)
	}

	const accepted = "1111111111111111111111111111111111111111"
	if _, err := transport.core.ImportWithHead(t.Context(), mirror.Snapshot{}, model.ReplicaHead{
		Replica: keyOther, SnapshotHead: accepted,
	}); err != nil {
		t.Fatalf("seed accepted head: %v", err)
	}

	forked := mirror.Snapshot{
		Predecessors: []string{"2222222222222222222222222222222222222222"},
		Records:      []mirror.Record{issueRecord(t, keyOther, 3, "aaaaa")},
	}
	head := commitSnapshot(t, transport, forked)

	if _, err := transport.importRemote(t.Context(), head); !errors.Is(err, ErrFork) {
		t.Fatalf("importRemote of a forked snapshot = %v, want ErrFork", err)
	}
	if _, err := transport.store.Issue(t.Context(), "aaaaa"); err == nil {
		t.Fatal("the forked snapshot's issue was written before the lineage was proved")
	}

	// The same records descending from the accepted head are not a fork.
	linear := forked
	linear.Predecessors = []string{accepted}
	if _, err := transport.importRemote(t.Context(), commitSnapshot(t, transport, linear)); err != nil {
		t.Fatalf("importRemote of a linear advance = %v, want the merge", err)
	}
	if _, err := transport.store.Issue(t.Context(), "aaaaa"); err != nil {
		t.Fatalf("linear advance did not land: %v", err)
	}
}

// TestImportRemoteRecordsTheSourcesHead: the next snapshot from this replica is
// checked against the head recorded here, so an import that merges without
// recording one disarms the fork proof from then on.
func TestImportRemoteRecordsTheSourcesHead(t *testing.T) {
	transport, _ := openTransportWith(t, Options{URL: bareRemote(t)})
	if err := (Sidecar{Replica: keyMine}).Save(transport.dir); err != nil {
		t.Fatal(err)
	}
	if err := transport.ensureRepo(t.Context()); err != nil {
		t.Fatal(err)
	}

	snapshot := mirror.Snapshot{Records: []mirror.Record{issueRecord(t, keyOther, 1, "aaaaa")}}
	head := commitSnapshot(t, transport, snapshot)
	if _, err := transport.importRemote(t.Context(), head); err != nil {
		t.Fatalf("importRemote: %v", err)
	}

	heads, err := transport.core.ReplicaHeads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, recorded := range heads {
		if recorded.Replica == keyOther && recorded.SnapshotHead == head {
			found = true
		}
	}
	if !found {
		t.Fatalf("replica heads %+v do not record %s at %s", heads, keyOther, head)
	}
}
