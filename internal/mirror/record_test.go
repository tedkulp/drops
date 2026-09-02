package mirror_test

import (
	"testing"

	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
)

func TestNewRecordDerivesKindFromConcreteType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		kind model.RecordKind
	}{
		{"project", validProject(), model.RecordProject},
		{"repository locator", validLocator(), model.RecordRepositoryLocator},
		{"issue", validIssue(), model.RecordIssue},
		{"issue parent", validIssueParent(), model.RecordIssueParent},
		{"dependency", validDependency(), model.RecordDependency},
		{"label", validLabel(), model.RecordLabel},
		{"comment", validComment(), model.RecordComment},
		{"memory", validMemory(), model.RecordMemory},
		{"replica succession", validSuccession(), model.RecordReplicaSuccession},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := mirror.NewRecord(tc.in)
			if err != nil {
				t.Fatalf("NewRecord(%T): %v", tc.in, err)
			}
			if rec.Kind != tc.kind {
				t.Errorf("kind = %q, want %q", rec.Kind, tc.kind)
			}
			if err := rec.Validate(); err != nil {
				t.Errorf("Validate(): %v", err)
			}
		})
	}
}

func TestNewRecordDereferencesPointers(t *testing.T) {
	t.Parallel()
	issue := validIssue()
	rec, err := mirror.NewRecord(&issue)
	if err != nil {
		t.Fatalf("NewRecord(*Issue): %v", err)
	}
	if rec.Kind != model.RecordIssue {
		t.Errorf("kind = %q, want %q", rec.Kind, model.RecordIssue)
	}
	got, ok := rec.Value.(model.Issue)
	if !ok {
		t.Fatalf("Value = %T, want model.Issue value", rec.Value)
	}
	if got.ID != issue.ID {
		t.Errorf("Value.ID = %q, want %q", got.ID, issue.ID)
	}
}

func TestNewRecordRejectsUnknownType(t *testing.T) {
	t.Parallel()
	for _, in := range []any{"nope", 42, nil, model.ID("k3f9x"), (*model.Issue)(nil)} {
		if _, err := mirror.NewRecord(in); err == nil {
			t.Errorf("NewRecord(%T) succeeded, want error", in)
		}
	}
}

func TestRecordRefCanonicalCompositeKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		want model.RecordRef
	}{
		{"project", validProject(), model.RecordRef{Kind: model.RecordProject, Key: string(model.GlobalProjectKey)}},
		{"repository locator", validLocator(), model.RecordRef{Kind: model.RecordRepositoryLocator, Key: string(model.GlobalProjectKey) + "\x00git@example.com:org/repo.git"}},
		{"issue", validIssue(), model.RecordRef{Kind: model.RecordIssue, Key: "br-6vf"}},
		{"issue parent", validIssueParent(), model.RecordRef{Kind: model.RecordIssueParent, Key: "dw32p.17"}},
		{"dependency", validDependency(), model.RecordRef{Kind: model.RecordDependency, Key: "br-6vf\x00k3f9x\x00blocks"}},
		{"label", validLabel(), model.RecordRef{Kind: model.RecordLabel, Key: "br-6vf\x00wayfinder:task"}},
		{"comment", validComment(), model.RecordRef{Kind: model.RecordComment, Key: "c1"}},
		{"memory", validMemory(), model.RecordRef{Kind: model.RecordMemory, Key: "mem-old-shape"}},
		{"replica succession", validSuccession(), model.RecordRef{Kind: model.RecordReplicaSuccession, Key: string(model.GlobalProjectKey) + "\x00" + string(model.InboxProjectKey)}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := mirror.NewRecord(tc.in)
			if err != nil {
				t.Fatalf("NewRecord(%T): %v", tc.in, err)
			}
			got, err := rec.Ref()
			if err != nil {
				t.Fatalf("Ref(): %v", err)
			}
			if got != tc.want {
				t.Errorf("Ref() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRecordValidateRejectsMismatchAndInvalid(t *testing.T) {
	t.Parallel()
	if err := (mirror.Record{Kind: model.RecordIssue, Value: validMemory()}).Validate(); err == nil {
		t.Error("mismatched Kind/Value accepted")
	}
	if err := (mirror.Record{Kind: model.RecordIssue, Value: nil}).Validate(); err == nil {
		t.Error("nil value accepted")
	}
	bad := validIssue()
	bad.ID = ""
	if err := (mirror.Record{Kind: model.RecordIssue, Value: bad}).Validate(); err == nil {
		t.Error("invalid issue accepted")
	}
}
