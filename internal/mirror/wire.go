package mirror

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"sort"

	json "encoding/json/v2"

	"github.com/tedkulp/drops/internal/model"
)

// ErrMalformed wraps every parse failure, so a caller can map the whole class
// to one exit code without matching message text.
var ErrMalformed = errors.New("mirror is malformed")

// snapshotKind is mirror's own envelope kind, distinct from the model's
// RecordKind vocabulary of replicated records.
const snapshotKind = "snapshot"

// Snapshot is one transport unit: the records a store exports in a single
// commit, plus the snapshot heads it descends from.
type Snapshot struct {
	Predecessors []string
	Records      []Record
}

// snapshotLine is the wire form of the snapshot header.
type snapshotLine struct {
	Kind         string   `json:"kind"`
	Predecessors []string `json:"predecessors"`
}

// Marshal renders a snapshot as deterministic JSONL: a snapshot header line,
// then one record per line. Records are emitted in canonical (kind, key) order
// and predecessors in ascending order, so two semantically equal snapshots
// produce identical bytes regardless of input order. Pure: no clock,
// filesystem, or randomness.
func Marshal(snapshot Snapshot) ([]byte, error) {
	items := make([]ordered, len(snapshot.Records))
	for i, rec := range snapshot.Records {
		if err := rec.Validate(); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		ref, err := rec.Ref()
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		items[i] = ordered{record: rec, ref: ref}
	}
	sort.SliceStable(items, func(i, j int) bool { return refLess(items[i].ref, items[j].ref) })
	for i := 1; i < len(items); i++ {
		if items[i-1].ref == items[i].ref {
			return nil, fmt.Errorf("%w: duplicate record %s %q", model.ErrInvalid, items[i].ref.Kind, items[i].ref.Key)
		}
	}

	predecessors := append([]string(nil), snapshot.Predecessors...)
	sort.Strings(predecessors)

	var buf bytes.Buffer
	header, err := json.Marshal(snapshotLine{Kind: snapshotKind, Predecessors: predecessors})
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot header: %w", err)
	}
	buf.Write(header)
	buf.WriteByte('\n')
	for _, item := range items {
		line, err := json.Marshal(item.record)
		if err != nil {
			return nil, fmt.Errorf("marshal record %s %q: %w", item.ref.Kind, item.ref.Key, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// Parse reads a mirror into a Snapshot, validating every line. It touches no
// database and no filesystem. Blank lines and a trailing carriage return are
// tolerated; everything else must be a well-formed header and typed records.
func Parse(data []byte) (Snapshot, error) {
	lines := splitLines(data)
	if len(lines) == 0 {
		return Snapshot{}, fmt.Errorf("%w: empty mirror", ErrMalformed)
	}

	var header snapshotLine
	if err := json.Unmarshal(lines[0], &header, json.RejectUnknownMembers(true)); err != nil {
		return Snapshot{}, lineError(1, "snapshot header: %v", err)
	}
	if header.Kind != snapshotKind {
		return Snapshot{}, lineError(1, "first line must be the snapshot header, got kind %q", header.Kind)
	}

	snapshot := Snapshot{Predecessors: header.Predecessors}
	seen := make(map[model.RecordRef]struct{})
	for i := 1; i < len(lines); i++ {
		rec, err := parseRecord(lines[i])
		if err != nil {
			return Snapshot{}, lineError(i+1, "%v", err)
		}
		ref, err := rec.Ref()
		if err != nil {
			return Snapshot{}, lineError(i+1, "%v", err)
		}
		if _, ok := seen[ref]; ok {
			return Snapshot{}, lineError(i+1, "duplicate record %s %q", ref.Kind, ref.Key)
		}
		seen[ref] = struct{}{}
		snapshot.Records = append(snapshot.Records, rec)
	}
	return snapshot, nil
}

// parseRecord decodes one non-header line into a typed, validated Record.
func parseRecord(line []byte) (Record, error) {
	var probe struct {
		Kind   string         `json:"kind"`
		Record jsontext.Value `json:"record"`
	}
	if err := json.Unmarshal(line, &probe, json.RejectUnknownMembers(true)); err != nil {
		return Record{}, fmt.Errorf("record line: %v", err)
	}
	kind := model.RecordKind(probe.Kind)
	if !model.ValidRecordKind(kind) {
		return Record{}, fmt.Errorf("unknown record kind %q", probe.Kind)
	}
	value, err := decodeValue(kind, probe.Record)
	if err != nil {
		return Record{}, err
	}
	rec := Record{Kind: kind, Value: value}
	if err := rec.Validate(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// decodeValue unmarshals a record payload into the model type for kind,
// rejecting unknown members so a stray field is not silently dropped.
func decodeValue(kind model.RecordKind, payload jsontext.Value) (any, error) {
	opts := json.RejectUnknownMembers(true)
	switch kind {
	case model.RecordProject:
		var v model.Project
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordRepositoryLocator:
		var v model.RepositoryLocator
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordIssue:
		var v model.Issue
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordIssueParent:
		var v model.IssueParent
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordDependency:
		var v model.Dependency
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordLabel:
		var v model.Label
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordComment:
		var v model.Comment
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordMemory:
		var v model.Memory
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	case model.RecordReplicaSuccession:
		var v model.ReplicaSuccession
		err := json.Unmarshal([]byte(payload), &v, opts)
		return v, err
	}
	return nil, fmt.Errorf("unknown record kind %q", kind)
}

type ordered struct {
	record Record
	ref    model.RecordRef
}

func refLess(a, b model.RecordRef) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Key < b.Key
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := bytes.TrimSuffix(raw, []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func lineError(n int, format string, args ...any) error {
	return fmt.Errorf("%w: line %d: %s", ErrMalformed, n, fmt.Sprintf(format, args...))
}
