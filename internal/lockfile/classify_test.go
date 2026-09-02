package lockfile

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// classify is the one distinction the whole package exists for: EWOULDBLOCK
// (another process holds the flock) must map to ErrHeld, and everything else —
// the errno a filesystem without flock support would actually return — must
// map to something a caller can tell apart from ErrHeld. This is tested
// directly against unix.Errno values so it does not depend on finding a real
// filesystem without flock support to provoke ENOTSUP/EOPNOTSUPP from.
func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		errno    error
		wantHeld bool
	}{
		{"EWOULDBLOCK is held", unix.EWOULDBLOCK, true},
		{"EAGAIN is held", unix.EAGAIN, true},
		{"ENOTSUP is not held", unix.ENOTSUP, false},
		{"EOPNOTSUPP is not held", unix.EOPNOTSUPP, false},
		{"EPERM is not held", unix.EPERM, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.errno)
			if got == nil {
				t.Fatal("classify returned nil for a flock failure")
			}
			if held := errors.Is(got, ErrHeld); held != tt.wantHeld {
				t.Errorf("classify(%v) = %v, errors.Is(_, ErrHeld) = %v, want %v",
					tt.errno, got, held, tt.wantHeld)
			}
		})
	}
}
