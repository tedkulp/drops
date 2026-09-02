package model

import (
	"fmt"
	"math"
	"strings"
)

// Revision orders versions of one replicated record. Generation is advanced
// from the version a writer observed; Replica deterministically orders
// concurrent revisions without using wall-clock time.
type Revision struct {
	Generation int64      `json:"generation"`
	Replica    ReplicaKey `json:"replica_key"`
}

func InitialRevision(replica ReplicaKey) (Revision, error) {
	if err := replica.Validate(); err != nil {
		return Revision{}, err
	}
	return Revision{Generation: 1, Replica: replica}, nil
}

func (revision Revision) Validate() error {
	if revision.Generation < 1 {
		return fmt.Errorf("%w: revision generation must be at least 1", ErrInvalid)
	}
	if err := revision.Replica.Validate(); err != nil {
		return fmt.Errorf("%w: revision replica: %v", ErrInvalid, err)
	}
	return nil
}

func (revision Revision) Next(replica ReplicaKey) (Revision, error) {
	if err := revision.Validate(); err != nil {
		return Revision{}, err
	}
	if err := replica.Validate(); err != nil {
		return Revision{}, err
	}
	if revision.Generation == math.MaxInt64 {
		return Revision{}, fmt.Errorf("%w: revision generation overflow", ErrInvalid)
	}
	return Revision{Generation: revision.Generation + 1, Replica: replica}, nil
}

// Compare returns -1, 0, or 1 using the deterministic revision order.
func (revision Revision) Compare(other Revision) int {
	if revision.Generation < other.Generation {
		return -1
	}
	if revision.Generation > other.Generation {
		return 1
	}
	return strings.Compare(string(revision.Replica), string(other.Replica))
}

// Concurrent reports whether two revisions were authored independently from
// the same observed generation.
func (revision Revision) Concurrent(other Revision) bool {
	return revision.Generation == other.Generation && revision.Replica != other.Replica
}

// Tombstone is the versioned liveness state of a removable replicated record.
type Tombstone bool

const (
	Live       Tombstone = false
	Tombstoned Tombstone = true
)

// RecordVersion pairs replicated content liveness with its revision.
type RecordVersion struct {
	Revision  Revision  `json:"revision"`
	Tombstone Tombstone `json:"tombstoned"`
}

func (version RecordVersion) Validate() error {
	return version.Revision.Validate()
}
