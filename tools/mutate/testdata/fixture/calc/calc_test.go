package calc

import "testing"

// Covers the strictness of Less: equal inputs must not be "less".
func TestLess(t *testing.T) {
	if Less(2, 2) {
		t.Error("Less(2, 2) = true, want false")
	}
	if !Less(1, 2) {
		t.Error("Less(1, 2) = false, want true")
	}
}

// Deliberately unable to fail for the lower bound: it only ever exercises the
// upper one. This is the defect the harness exists to find.
func TestClamp(t *testing.T) {
	if got := Clamp(9, 0, 5); got != 5 {
		t.Errorf("Clamp(9, 0, 5) = %d, want 5", got)
	}
}
