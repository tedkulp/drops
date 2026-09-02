package model_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

func TestMergeConflictDescribesDeterministicWinner(t *testing.T) {
	t.Parallel()

	low := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	high := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")
	record := model.RecordRef{Kind: model.RecordIssue, Key: "br-6vf"}
	current := model.RecordVersion{
		Revision:  model.Revision{Generation: 7, Replica: low},
		Tombstone: model.Live,
	}
	incoming := model.RecordVersion{
		Revision:  model.Revision{Generation: 7, Replica: high},
		Tombstone: model.Tombstoned,
	}

	conflict := model.MergeConflict{
		Record:     record,
		Current:    current,
		Incoming:   incoming,
		Chosen:     incoming,
		DeleteWins: true,
	}
	if err := conflict.Validate(); err != nil {
		t.Fatalf("valid delete-wins conflict: %v", err)
	}

	conflict.Chosen = current
	assertInvalid(t, "live record chosen over tombstone", conflict.Validate())
	conflict.Chosen = incoming
	conflict.DeleteWins = false
	assertInvalid(t, "delete-wins not reported", conflict.Validate())
}

func TestMergeConflictUsesReplicaOrderWhenLivenessMatches(t *testing.T) {
	t.Parallel()

	low := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	high := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")
	current := model.RecordVersion{Revision: model.Revision{Generation: 3, Replica: low}}
	incoming := model.RecordVersion{Revision: model.Revision{Generation: 3, Replica: high}}
	conflict := model.MergeConflict{
		Record:   model.RecordRef{Kind: model.RecordLabel, Key: "br-6vf\x00wayfinder:map"},
		Current:  current,
		Incoming: incoming,
		Chosen:   incoming,
	}
	if err := conflict.Validate(); err != nil {
		t.Fatalf("valid replica-ordered conflict: %v", err)
	}

	conflict.Chosen = current
	assertInvalid(t, "lower replica chosen", conflict.Validate())
}

func TestMergeConflictRequiresConcurrentRevisions(t *testing.T) {
	t.Parallel()

	key := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	version := model.RecordVersion{Revision: model.Revision{Generation: 1, Replica: key}}
	conflict := model.MergeConflict{
		Record:   model.RecordRef{Kind: model.RecordMemory, Key: "mem-old-shape"},
		Current:  version,
		Incoming: version,
		Chosen:   version,
	}
	assertInvalid(t, "same revision reported as conflict", conflict.Validate())
}

func TestHardMirrorConflictsRetainFactsAndStableClass(t *testing.T) {
	t.Parallel()

	low := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	high := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")
	record := model.RecordRef{Kind: model.RecordIssue, Key: "br-6vf"}

	identity := model.IdentityCollision{
		Record:          record,
		CurrentOrigin:   low,
		IncomingOrigin:  high,
		CurrentSummary:  "current title",
		IncomingSummary: "incoming title",
	}
	if err := identity.Validate(); err != nil {
		t.Fatalf("valid identity collision: %v", err)
	}
	if !errors.Is(identity, model.ErrConflict) {
		t.Error("IdentityCollision does not match ErrConflict")
	}
	for _, fact := range []string{"br-6vf", string(low), string(high), "current title", "incoming title"} {
		if !strings.Contains(identity.Error(), fact) {
			t.Errorf("IdentityCollision error omits %q: %s", fact, identity.Error())
		}
	}

	content := model.ContentConflict{
		Record:          record,
		Revision:        model.Revision{Generation: 4, Replica: low},
		CurrentSummary:  "sha256:aaaa",
		IncomingSummary: "sha256:bbbb",
	}
	if err := content.Validate(); err != nil {
		t.Fatalf("valid content conflict: %v", err)
	}
	if !errors.Is(content, model.ErrConflict) {
		t.Error("ContentConflict does not match ErrConflict")
	}
}

func TestConflictFactsRejectMalformedRecords(t *testing.T) {
	t.Parallel()

	key := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	other := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")
	assertInvalid(t, "unknown record kind", (model.RecordRef{Kind: "workspace", Key: "/tmp/repo"}).Validate())
	assertInvalid(t, "empty record key", (model.RecordRef{Kind: model.RecordIssue}).Validate())
	assertInvalid(t, "immutable Comment reported as merge conflict", (model.MergeConflict{
		Record:   model.RecordRef{Kind: model.RecordComment, Key: "br-6vf:acd91ca2f7b0"},
		Current:  model.RecordVersion{Revision: model.Revision{Generation: 1, Replica: key}},
		Incoming: model.RecordVersion{Revision: model.Revision{Generation: 1, Replica: other}},
		Chosen:   model.RecordVersion{Revision: model.Revision{Generation: 1, Replica: other}},
	}).Validate())
	assertInvalid(t, "same identity origin", (model.IdentityCollision{
		Record:          model.RecordRef{Kind: model.RecordIssue, Key: "br-6vf"},
		CurrentOrigin:   key,
		IncomingOrigin:  key,
		CurrentSummary:  "a",
		IncomingSummary: "b",
	}).Validate())
	assertInvalid(t, "set record reported as identity collision", (model.IdentityCollision{
		Record:          model.RecordRef{Kind: model.RecordLabel, Key: "br-6vf\\x00wayfinder:map"},
		CurrentOrigin:   key,
		IncomingOrigin:  other,
		CurrentSummary:  "a",
		IncomingSummary: "b",
	}).Validate())
}
