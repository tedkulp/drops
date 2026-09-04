package main

import (
	"os"
	"os/exec"
	"testing"
)

// TestTheTreeCompilesForDarwin is a toolchain test on purpose. The defect it
// guards is a Linux-only constant or syscall reaching production code, and no
// unit test can go red for that: the line compiles and behaves identically on
// the machine running the suite both before and after it is written. Only a
// compiler pointed at another GOOS can fail for it.
//
// `vet` rather than `build` because vet type-checks the _test files too, so a
// Linux-only import that only a test reaches is caught here as well.
//
// drops is UNIX-only by design — flock, ioctl, no exec("tty") — and darwin is
// the second UNIX it is actually run on. Both spellings of every ioctl this
// tree issues therefore have to exist, which is what this asserts.
func TestTheTreeCompilesForDarwin(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH to cross-check with")
	}
	// An absolute package pattern, so the check does not depend on the working
	// directory the runner chose.
	cmd := exec.Command("go", "vet", "github.com/tedkulp/drops/...")
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the tree does not build for darwin (macOS): %v\n%s", err, out)
	}
}
