package model_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/model"
)

func mustReplicaKey(t *testing.T, raw string) model.ReplicaKey {
	t.Helper()
	key, err := model.ParseReplicaKey(raw)
	if err != nil {
		t.Fatalf("ParseReplicaKey(%q): %v", raw, err)
	}
	return key
}

func TestTimestampValidationPreservesRFC3339NanoBytes(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"2026-08-15T02:49:07Z",
		"2026-08-15T02:49:07.863286000Z",
		"2026-08-15T02:49:07.1Z",
	} {
		stamp, err := model.ParseTimestamp(raw)
		if err != nil {
			t.Errorf("ParseTimestamp(%q): %v", raw, err)
			continue
		}
		if string(stamp) != raw {
			t.Errorf("ParseTimestamp(%q) = %q; bytes changed", raw, stamp)
		}
	}

	for _, raw := range []string{
		"",
		"2026-08-15",
		"2026-08-15T02:49:07+00:00",
		"2026-08-15T02:49:07.1234567890Z",
		"2026-02-30T02:49:07Z",
	} {
		if _, err := model.ParseTimestamp(raw); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("ParseTimestamp(%q) error = %v, want ErrInvalid", raw, err)
		}
	}
}

func TestNewTimestampWritesUTC(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 9, 2, 12, 34, 56, 123_000_000, time.FixedZone("east", 5*60*60))
	if got, want := model.NewTimestamp(instant), model.Timestamp("2026-09-02T07:34:56.123Z"); got != want {
		t.Errorf("NewTimestamp = %q, want %q", got, want)
	}
}

func TestRevisionLifecycleAndOrdering(t *testing.T) {
	t.Parallel()

	low := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	high := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")

	initial, err := model.InitialRevision(low)
	if err != nil {
		t.Fatalf("InitialRevision: %v", err)
	}
	if initial.Generation != 1 || initial.Replica != low {
		t.Fatalf("InitialRevision = %+v", initial)
	}

	next, err := initial.Next(high)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next.Generation != 2 || next.Replica != high {
		t.Fatalf("Next = %+v", next)
	}
	if initial.Compare(next) >= 0 || next.Compare(initial) <= 0 {
		t.Errorf("generation did not determine order: initial=%+v next=%+v", initial, next)
	}

	concurrentLow := model.Revision{Generation: 7, Replica: low}
	concurrentHigh := model.Revision{Generation: 7, Replica: high}
	if !concurrentLow.Concurrent(concurrentHigh) {
		t.Error("equal-generation revisions from different replicas are not concurrent")
	}
	if concurrentLow.Compare(concurrentHigh) >= 0 {
		t.Error("replica key did not deterministically break equal-generation tie")
	}
	if concurrentLow.Concurrent(concurrentLow) {
		t.Error("one revision is concurrent with itself")
	}
}

func TestRevisionRejectsInvalidState(t *testing.T) {
	t.Parallel()

	validKey := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	for name, revision := range map[string]model.Revision{
		"zero generation": {Replica: validKey},
		"missing replica": {Generation: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := revision.Validate(); !errors.Is(err, model.ErrInvalid) {
				t.Errorf("Validate error = %v, want ErrInvalid", err)
			}
		})
	}

	max := model.Revision{Generation: math.MaxInt64, Replica: validKey}
	if _, err := max.Next(validKey); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("overflow Next error = %v, want ErrInvalid", err)
	}
}

func TestStableErrorsSurviveContextWrapping(t *testing.T) {
	t.Parallel()

	for _, target := range []error{model.ErrInvalid, model.ErrNotFound, model.ErrConflict} {
		wrapped := errors.Join(errors.New("operation context"), target)
		if !errors.Is(wrapped, target) {
			t.Errorf("wrapped error no longer matches %v", target)
		}
	}
}
