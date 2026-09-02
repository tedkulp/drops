package core

import (
	"sync"
	"time"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// Clock is the only time seam. Core makes its readings strictly monotonic before
// they become user-visible timestamps.
type Clock interface {
	Now() time.Time
}

// IDSource supplies opaque Issue and Memory ID candidates. Reserved carries
// every permanently owned ID; parent is non-nil only for a child Issue.
type IDSource interface {
	NewID(kind model.OwnerKind, parent *model.ID, reserved []model.ID) (model.ID, error)
}

// Warning is advisory write-time information. It never changes whether the
// write commits.
type Warning struct {
	Entity model.RecordRef `json:"entity"`
	Field  string          `json:"field"`
	Family string          `json:"family"`
}

// WarnSink receives advisory warnings after a successful commit.
type WarnSink func(Warning)

// Core owns domain rules and transaction boundaries over one Store.
type Core struct {
	store   *store.Store
	replica model.ReplicaKey
	clock   Clock
	ids     IDSource
	warn    WarnSink

	clockMu sync.Mutex
	idMu    sync.Mutex
	warnMu  sync.Mutex
	last    time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

// New builds the rules module. Nil collaborators select production defaults or,
// for the warning sink, discard advisory warnings.
func New(opened *store.Store, replica model.ReplicaKey, clock Clock, ids IDSource, warn WarnSink) *Core {
	if clock == nil {
		clock = wallClock{}
	}
	if ids == nil {
		ids = randomIDs{}
	}
	return &Core{store: opened, replica: replica, clock: clock, ids: ids, warn: warn}
}

func (core *Core) timestamp() model.Timestamp {
	core.clockMu.Lock()
	defer core.clockMu.Unlock()

	now := core.clock.Now().UTC()
	if !now.After(core.last) {
		now = core.last.Add(time.Nanosecond)
	}
	core.last = now
	return model.NewTimestamp(now)
}

func (core *Core) currentTime() time.Time {
	core.clockMu.Lock()
	defer core.clockMu.Unlock()
	return core.clock.Now()
}

func isReservedProject(key model.ProjectKey) bool {
	return key == model.GlobalProjectKey || key == model.InboxProjectKey
}

func isReservedSlug(slug string) bool {
	return slug == "global" || slug == "inbox"
}
