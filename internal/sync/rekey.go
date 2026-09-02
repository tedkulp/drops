package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// ErrNoActiveKey reports a rekey on a store that has not yet minted a replica
// key, where there is nothing to rotate.
var ErrNoActiveKey = errors.New("no active replica key to rekey")

// Rekey rotates the active replica key while holding the store lock: it records
// the old-to-new succession for mirror diagnostics and replaces the sidecar key,
// leaving every creation origin and revision already written untouched. It is
// the repair a duplicated or restored key needs before either copy writes again.
func (t *Transport) Rekey(ctx context.Context) (model.ReplicaKey, error) {
	release, _, err := t.withLock(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	sidecar, err := LoadSidecar(t.dir)
	if err != nil {
		return "", err
	}
	if sidecar == nil {
		return "", ErrNoActiveKey
	}

	next, err := model.NewReplicaKey()
	if err != nil {
		return "", err
	}
	if next == sidecar.Replica {
		return "", fmt.Errorf("generate replica key: collision")
	}

	if _, err := t.core.RecordReplicaSuccession(ctx, sidecar.Replica, next); err != nil {
		return "", err
	}
	sidecar.Replica = next
	if err := sidecar.Save(t.dir); err != nil {
		return "", err
	}
	return next, nil
}
