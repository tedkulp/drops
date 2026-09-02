package model

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

const keyBytes = 16

var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ID is an opaque identifier from the shared Issue/Memory namespace. Its only
// spelling invariant is non-emptiness; legacy shapes are permanent.
type ID string

func ParseID(raw string) (ID, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty ID", ErrInvalid)
	}
	return ID(raw), nil
}

func (id ID) Validate() error {
	_, err := ParseID(string(id))
	return err
}

// ProjectKey is a Project's immutable identity across stores.
type ProjectKey string

// ReplicaKey is the immutable identity of one writable-store lineage.
type ReplicaKey string

// Reserved Project keys are fixed so independently created stores converge.
const (
	GlobalProjectKey ProjectKey = "jvxtjayrcs7vdguuqche7njh2y"
	InboxProjectKey  ProjectKey = "h2sbybyal4ryua23cbfb5wvawq"

	GlobalProjectSlug = "global"
	InboxProjectSlug  = "inbox"
)

func NewProjectKey() (ProjectKey, error) {
	key, err := newKey()
	return ProjectKey(key), err
}

func NewReplicaKey() (ReplicaKey, error) {
	key, err := newKey()
	return ReplicaKey(key), err
}

func newKey() (string, error) {
	var raw [keyBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return strings.ToLower(keyEncoding.EncodeToString(raw[:])), nil
}

func ParseProjectKey(raw string) (ProjectKey, error) {
	if err := validateKey(raw); err != nil {
		return "", fmt.Errorf("project key: %w", err)
	}
	return ProjectKey(raw), nil
}

func ParseReplicaKey(raw string) (ReplicaKey, error) {
	if err := validateKey(raw); err != nil {
		return "", fmt.Errorf("replica key: %w", err)
	}
	return ReplicaKey(raw), nil
}

func (key ProjectKey) Validate() error {
	_, err := ParseProjectKey(string(key))
	return err
}

func (key ReplicaKey) Validate() error {
	_, err := ParseReplicaKey(string(key))
	return err
}

func validateKey(raw string) error {
	if len(raw) != 26 || raw != strings.ToLower(raw) {
		return fmt.Errorf("%w: key must be 26 lowercase Base32 characters", ErrInvalid)
	}
	decoded, err := keyEncoding.DecodeString(strings.ToUpper(raw))
	if err != nil || len(decoded) != keyBytes || strings.ToLower(keyEncoding.EncodeToString(decoded)) != raw {
		return fmt.Errorf("%w: key must be canonical unpadded 128-bit Base32", ErrInvalid)
	}
	return nil
}
