package mirror_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
)

func mustRecord(t *testing.T, v any) mirror.Record {
	t.Helper()
	rec, err := mirror.NewRecord(v)
	if err != nil {
		t.Fatalf("NewRecord(%T): %v", v, err)
	}
	return rec
}

func sortedByRef(t *testing.T, records []mirror.Record) []mirror.Record {
	t.Helper()
	out := append([]mirror.Record(nil), records...)
	sort.Slice(out, func(i, j int) bool {
		ri, err := out[i].Ref()
		if err != nil {
			t.Fatal(err)
		}
		rj, err := out[j].Ref()
		if err != nil {
			t.Fatal(err)
		}
		if ri.Kind != rj.Kind {
			return ri.Kind < rj.Kind
		}
		return ri.Key < rj.Key
	})
	return out
}

func wireLines(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

func TestMarshalSnapshotHeaderExactBytes(t *testing.T) {
	t.Parallel()
	snap := mirror.Snapshot{Records: []mirror.Record{mustRecord(t, validComment())}}
	got, err := mirror.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "{\"kind\":\"snapshot\",\"predecessors\":[]}\n" +
		"{\"kind\":\"comment\",\"record\":{\"id\":\"c1\",\"issue_id\":\"br-6vf\",\"author\":\"opencode\",\"body\":\"a note\",\"created_at\":\"2026-09-02T07:34:56.123Z\",\"creation_replica\":\"jvxtjayrcs7vdguuqche7njh2y\"}}\n"
	if string(got) != want {
		t.Errorf("Marshal bytes:\n got: %q\nwant: %q", got, want)
	}
}

func TestMarshalSortsRecordsAndPredecessors(t *testing.T) {
	t.Parallel()
	records := []mirror.Record{
		mustRecord(t, validProject()),
		mustRecord(t, validIssue()),
		mustRecord(t, validMemory()),
		mustRecord(t, validDependency()),
	}
	reversed := append([]mirror.Record(nil), records...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	a, err := mirror.Marshal(mirror.Snapshot{Predecessors: []string{"deadbeef", "cafebabe"}, Records: records})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	b, err := mirror.Marshal(mirror.Snapshot{Predecessors: []string{"cafebabe", "deadbeef"}, Records: reversed})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("Marshal is not deterministic across record/predecessor order:\n%q\n%q", a, b)
	}

	// Canonical order: dependency < issue < memory < project; predecessors sorted.
	text := string(a)
	wantOrder := []string{"\"kind\":\"dependency\"", "\"kind\":\"issue\"", "\"kind\":\"memory\"", "\"kind\":\"project\""}
	last := -1
	for _, want := range wantOrder {
		pos := strings.Index(text, want)
		if pos < 0 {
			t.Fatalf("missing %s in output: %s", want, text)
		}
		if pos < last {
			t.Errorf("record %s out of order in %s", want, text)
		}
		last = pos
	}
	if !strings.Contains(text, "\"predecessors\":[\"cafebabe\",\"deadbeef\"]") {
		t.Errorf("predecessors not sorted: %s", text)
	}
}

func TestRoundTripAllRecordKinds(t *testing.T) {
	t.Parallel()
	records := []mirror.Record{
		mustRecord(t, validProject()),
		mustRecord(t, validLocator()),
		mustRecord(t, validIssue()),
		mustRecord(t, validIssueParent()),
		mustRecord(t, validDependency()),
		mustRecord(t, validLabel()),
		mustRecord(t, validComment()),
		mustRecord(t, validMemory()),
		mustRecord(t, validSuccession()),
	}
	snap := mirror.Snapshot{Predecessors: []string{"abc123", "def456"}, Records: records}

	data, err := mirror.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := mirror.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !reflect.DeepEqual(got.Predecessors, snap.Predecessors) {
		t.Errorf("predecessors = %v, want %v", got.Predecessors, snap.Predecessors)
	}
	want := sortedByRef(t, records)
	have := sortedByRef(t, got.Records)
	if len(have) != len(want) {
		t.Fatalf("got %d records, want %d", len(have), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(have[i], want[i]) {
			t.Errorf("record %d:\n got %#v\nwant %#v", i, have[i], want[i])
		}
	}
}

func TestRoundTripTombstoneAndAdvancedRevision(t *testing.T) {
	t.Parallel()
	issue := validIssue()
	issue.Tombstone = model.Tombstoned
	issue.Revision = model.Revision{Generation: 7, Replica: replicaB}
	mem := validMemory()
	mem.Tombstone = model.Tombstoned

	data, err := mirror.Marshal(mirror.Snapshot{Records: []mirror.Record{
		mustRecord(t, issue),
		mustRecord(t, mem),
	}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := mirror.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byRef := map[string]mirror.Record{}
	for _, r := range got.Records {
		ref, err := r.Ref()
		if err != nil {
			t.Fatal(err)
		}
		byRef[ref.Key] = r
	}
	gotIssue := byRef["br-6vf"].Value.(model.Issue)
	if gotIssue.Tombstone != model.Tombstoned {
		t.Errorf("issue tombstone = %v, want tombstoned", gotIssue.Tombstone)
	}
	if gotIssue.Revision != (model.Revision{Generation: 7, Replica: replicaB}) {
		t.Errorf("issue revision = %+v, want generation 7 under replicaB", gotIssue.Revision)
	}
	gotMem := byRef["mem-old-shape"].Value.(model.Memory)
	if gotMem.Tombstone != model.Tombstoned {
		t.Errorf("memory tombstone = %v, want tombstoned", gotMem.Tombstone)
	}
}

func TestCommentWireOmitsRevisionAndTombstone(t *testing.T) {
	t.Parallel()
	data, err := mirror.Marshal(mirror.Snapshot{Records: []mirror.Record{mustRecord(t, validComment())}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lines := wireLines(t, data)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
	}
	recordLine := string(lines[1])
	if strings.Contains(recordLine, "revision") {
		t.Errorf("comment line carries a revision field: %s", recordLine)
	}
	if strings.Contains(recordLine, "tombstoned") {
		t.Errorf("comment line carries a tombstone field: %s", recordLine)
	}
	// The derived revision must equal (1, creation_replica).
	got, err := mirror.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	comment := got.Records[0].Value.(model.Comment)
	if want := (model.Revision{Generation: 1, Replica: replicaA}); comment.Revision() != want {
		t.Errorf("comment.Revision() = %+v, want %+v", comment.Revision(), want)
	}
}

func TestLegacyIDShapesRoundTripByteForByte(t *testing.T) {
	t.Parallel()
	ids := []model.ID{"k3f9x", "br-6vf", "beacon-ci-never-executed-4bhg"}
	memIDs := []model.ID{"mem-abc12", "gitops-r1w"}

	var records []mirror.Record
	for _, id := range ids {
		issue := validIssue()
		issue.ID = id
		records = append(records, mustRecord(t, issue))
	}
	for _, id := range memIDs {
		mem := validMemory()
		mem.ID = id
		records = append(records, mustRecord(t, mem))
	}

	data, err := mirror.Marshal(mirror.Snapshot{Records: records})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(data)
	for _, id := range append(append([]model.ID{}, ids...), memIDs...) {
		if !strings.Contains(text, `"id":"`+string(id)+`"`) {
			t.Errorf("wire does not carry id %q verbatim: %s", id, text)
		}
	}
	got, err := mirror.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	gotIDs := map[string]bool{}
	for _, r := range got.Records {
		ref, err := r.Ref()
		if err != nil {
			t.Fatal(err)
		}
		gotIDs[ref.Key] = true
	}
	for _, id := range append(append([]model.ID{}, ids...), memIDs...) {
		if !gotIDs[string(id)] {
			t.Errorf("id %q did not round-trip", id)
		}
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"header missing", "{\"kind\":\"issue\",\"record\":{\"id\":\"x\"}}\n"},
		{"bad json", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n{\"kind\":\n"},
		{"no kind", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n{\"record\":{}}\n"},
		{"unknown kind", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n{\"kind\":\"bogus\",\"record\":{}}\n"},
		{"snapshot kind as record", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n{\"kind\":\"snapshot\",\"predecessors\":[]}\n"},
		{"duplicate record", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n" +
			"{\"kind\":\"issue\",\"record\":{\"id\":\"a\",\"project_key\":\"jvxtjayrcs7vdguuqche7njh2y\",\"title\":\"t\",\"issue_type\":\"task\",\"status\":\"open\",\"priority\":2,\"created_at\":\"2026-09-02T07:34:56.123Z\",\"updated_at\":\"2026-09-02T07:34:56.123Z\",\"tombstoned\":false,\"creation_replica\":\"jvxtjayrcs7vdguuqche7njh2y\",\"revision\":{\"generation\":1,\"replica_key\":\"jvxtjayrcs7vdguuqche7njh2y\"}}}\n" +
			"{\"kind\":\"issue\",\"record\":{\"id\":\"a\",\"project_key\":\"jvxtjayrcs7vdguuqche7njh2y\",\"title\":\"t2\",\"issue_type\":\"task\",\"status\":\"open\",\"priority\":2,\"created_at\":\"2026-09-02T07:34:56.123Z\",\"updated_at\":\"2026-09-02T07:34:56.123Z\",\"tombstoned\":false,\"creation_replica\":\"jvxtjayrcs7vdguuqche7njh2y\",\"revision\":{\"generation\":1,\"replica_key\":\"jvxtjayrcs7vdguuqche7njh2y\"}}}\n"},
		{"invalid record", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n" +
			"{\"kind\":\"issue\",\"record\":{\"id\":\"a\",\"project_key\":\"jvxtjayrcs7vdguuqche7njh2y\",\"title\":\"\",\"issue_type\":\"task\",\"status\":\"open\",\"priority\":2,\"created_at\":\"2026-09-02T07:34:56.123Z\",\"updated_at\":\"2026-09-02T07:34:56.123Z\",\"tombstoned\":false,\"creation_replica\":\"jvxtjayrcs7vdguuqche7njh2y\",\"revision\":{\"generation\":1,\"replica_key\":\"jvxtjayrcs7vdguuqche7njh2y\"}}}\n"},
		{"unknown member in record", "{\"kind\":\"snapshot\",\"predecessors\":[]}\n{\"kind\":\"issue\",\"record\":{\"id\":\"a\",\"bogus\":1}}\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := mirror.Parse([]byte(tc.data)); !errors.Is(err, mirror.ErrMalformed) {
				t.Errorf("Parse(%q) error = %v, want ErrMalformed", tc.data, err)
			}
		})
	}
}

func TestParseToleratesBlankLinesAndCRLF(t *testing.T) {
	t.Parallel()
	data := []byte("{\"kind\":\"snapshot\",\"predecessors\":[]}\r\n\r\n" +
		"{\"kind\":\"comment\",\"record\":{\"id\":\"c1\",\"issue_id\":\"br-6vf\",\"author\":\"opencode\",\"body\":\"a note\",\"created_at\":\"2026-09-02T07:34:56.123Z\",\"creation_replica\":\"jvxtjayrcs7vdguuqche7njh2y\"}}\r\n")
	got, err := mirror.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(got.Records))
	}
	if got.Records[0].Value.(model.Comment).ID != "c1" {
		t.Errorf("comment id = %q", got.Records[0].Value.(model.Comment).ID)
	}
}

func TestIndependentDecodeIssueAndSuccession(t *testing.T) {
	t.Parallel()
	issue := validIssue()
	issue.ID = "beacon-ci-never-executed-4bhg"
	issue.Title = "Title with <angle> & ampersand"
	issue.Priority = 4
	issue.Assignee = strPtr("opencode")
	issue.Tombstone = model.Tombstoned

	snap := mirror.Snapshot{
		Predecessors: []string{"abc123"},
		Records: []mirror.Record{
			mustRecord(t, issue),
			mustRecord(t, validSuccession()),
		},
	}
	data, err := mirror.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lines := wireLines(t, data)
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), data)
	}

	// Decode the issue line with the standard library, not mirror's decoder.
	var envelope struct {
		Kind   string          `json:"kind"`
		Record json.RawMessage `json:"record"`
	}
	if err := json.Unmarshal(lines[1], &envelope); err != nil {
		t.Fatalf("std json unmarshal envelope: %v", err)
	}
	if envelope.Kind != "issue" {
		t.Errorf("kind = %q, want issue", envelope.Kind)
	}
	var got struct {
		ID         string `json:"id"`
		ProjectKey string `json:"project_key"`
		Title      string `json:"title"`
		Type       string `json:"issue_type"`
		Status     string `json:"status"`
		Priority   int    `json:"priority"`
		Assignee   string `json:"assignee"`
		Tombstoned bool   `json:"tombstoned"`
		Revision   struct {
			Generation int64  `json:"generation"`
			ReplicaKey string `json:"replica_key"`
		} `json:"revision"`
	}
	if err := json.Unmarshal(envelope.Record, &got); err != nil {
		t.Fatalf("std json unmarshal record: %v", err)
	}
	if got.ID != "beacon-ci-never-executed-4bhg" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Title != "Title with <angle> & ampersand" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Priority != 4 {
		t.Errorf("priority = %d, want 4", got.Priority)
	}
	if got.Assignee != "opencode" {
		t.Errorf("assignee = %q", got.Assignee)
	}
	if !got.Tombstoned {
		t.Errorf("tombstoned = false, want true")
	}
	if got.Revision.Generation != 1 || got.Revision.ReplicaKey != string(replicaA) {
		t.Errorf("revision = %+v", got.Revision)
	}

	// Succession line: old_replica_key -> new_replica_key.
	if err := json.Unmarshal(lines[2], &envelope); err != nil {
		t.Fatalf("std json unmarshal succession envelope: %v", err)
	}
	var succession struct {
		OldReplicaKey string `json:"old_replica_key"`
		NewReplicaKey string `json:"new_replica_key"`
		RekeyedAt     string `json:"rekeyed_at"`
	}
	if err := json.Unmarshal(envelope.Record, &succession); err != nil {
		t.Fatalf("std json unmarshal succession: %v", err)
	}
	if succession.OldReplicaKey != string(replicaA) || succession.NewReplicaKey != string(replicaB) {
		t.Errorf("succession keys = %q -> %q, want %q -> %q",
			succession.OldReplicaKey, succession.NewReplicaKey, replicaA, replicaB)
	}
}

// TestIndependentDecodeEveryRecordKind decodes the real writer's bytes with the
// standard library, keyed by hand-written field names, so a wrong tag or a
// missing discriminator cannot pass by agreement with mirror's own decoder.
func TestIndependentDecodeEveryRecordKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		rec      any
		kind     string
		required []string
		absent   []string
	}{
		{"project", validProject(), "project",
			[]string{"project_key", "slug", "created_at", "updated_at", "creation_replica", "revision"}, nil},
		{"repository locator", validLocator(), "repository_locator",
			[]string{"project_key", "locator", "tombstoned", "revision"}, nil},
		{"issue", validIssue(), "issue",
			[]string{"id", "project_key", "title", "description", "issue_type", "status", "priority", "created_at", "updated_at", "tombstoned", "creation_replica", "revision"}, nil},
		{"issue parent", validIssueParent(), "issue_parent",
			[]string{"child_id", "parent_id", "created_at", "tombstoned", "revision"}, nil},
		{"dependency", validDependency(), "dependency",
			[]string{"from_id", "to_id", "dep_type", "created_at", "tombstoned", "revision"}, nil},
		{"label", validLabel(), "label",
			[]string{"issue_id", "label", "tombstoned", "revision"}, nil},
		{"comment", validComment(), "comment",
			[]string{"id", "issue_id", "author", "body", "created_at", "creation_replica"},
			[]string{"revision", "tombstoned"}},
		{"memory", validMemory(), "memory",
			[]string{"id", "project_key", "title", "body", "created_at", "updated_at", "tombstoned", "creation_replica", "revision"}, nil},
		{"replica succession", validSuccession(), "replica_succession",
			[]string{"old_replica_key", "new_replica_key", "rekeyed_at"}, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := mirror.Marshal(mirror.Snapshot{Records: []mirror.Record{mustRecord(t, tc.rec)}})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			lines := wireLines(t, data)
			if len(lines) != 2 {
				t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
			}

			var envelope struct {
				Kind   string          `json:"kind"`
				Record json.RawMessage `json:"record"`
			}
			if err := json.Unmarshal(lines[1], &envelope); err != nil {
				t.Fatalf("std json unmarshal envelope: %v", err)
			}
			if envelope.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", envelope.Kind, tc.kind)
			}

			var payload map[string]any
			if err := json.Unmarshal(envelope.Record, &payload); err != nil {
				t.Fatalf("std json unmarshal record: %v", err)
			}
			for _, key := range tc.required {
				if _, ok := payload[key]; !ok {
					t.Errorf("missing required field %q in %s", key, envelope.Record)
				}
			}
			for _, key := range tc.absent {
				if _, ok := payload[key]; ok {
					t.Errorf("unexpected field %q present in %s", key, envelope.Record)
				}
			}
		})
	}
}

func strPtr(s string) *string { return &s }
