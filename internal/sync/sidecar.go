package sync

import (
	"fmt"
	"os"
	"path/filepath"

	json "encoding/json/v2"

	"github.com/tedkulp/drops/internal/model"
)

const (
	// mirrorFileName is the one file that crosses machines: the deterministic
	// JSONL snapshot every store names on export and reads on import.
	mirrorFileName = "mirror.jsonl"
	// sidecarFileName is the machine-local replica identity file.
	sidecarFileName = "replica.json"
	// lockFileName serialises a sync across processes.
	lockFileName = ".sync.lock"
)

// Sidecar is one installation's local replica identity: the active replica key
// and the head of its last export. The key orders concurrent revisions and
// distinguishes an update from an identifier collision; the head pairs against
// the store's own record of the last export so a torn restore refuses local
// authorship rather than silently writing under a stale identity.
type Sidecar struct {
	Replica        model.ReplicaKey `json:"replica_key"`
	LastExportHead string           `json:"last_export_head,omitempty"`
}

// Validate rejects a sidecar whose key is not a valid replica key.
func (sidecar Sidecar) Validate() error {
	if err := sidecar.Replica.Validate(); err != nil {
		return fmt.Errorf("replica sidecar: %w", err)
	}
	return nil
}

// LoadSidecar reads the sidecar beside one store. An absent file is a missing
// key, reported as (nil, nil): a store that has never authored locally has no
// replica and is still readable and importable. A present but malformed or
// invalid sidecar is a hard error, never silently replaced.
func LoadSidecar(dir string) (*Sidecar, error) {
	path := filepath.Join(dir, sidecarFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sidecar %s: %w", path, err)
	}
	var sidecar Sidecar
	if err := json.Unmarshal(raw, &sidecar, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("sidecar %s: %w", path, err)
	}
	if err := sidecar.Validate(); err != nil {
		return nil, fmt.Errorf("sidecar %s: %w", path, err)
	}
	return &sidecar, nil
}

// MintSidecar writes a fresh sidecar carrying a newly generated replica key and
// no export head, returning it for the caller to author under.
func MintSidecar(dir string) (*Sidecar, error) {
	key, err := model.NewReplicaKey()
	if err != nil {
		return nil, err
	}
	sidecar := &Sidecar{Replica: key}
	if err := sidecar.Save(dir); err != nil {
		return nil, err
	}
	return sidecar, nil
}

// Save writes the sidecar atomically beside the store.
func (sidecar Sidecar) Save(dir string) error {
	if err := sidecar.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(sidecar)
	if err != nil {
		return fmt.Errorf("encode sidecar: %w", err)
	}
	return writeFileAtomic(dir, sidecarFileName, append(raw, '\n'))
}
