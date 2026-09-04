package core

import "testing"

// TestNaturalLessComparesDigitRunsNumerically is the comparator that orders a
// page's relations. It has three branches and each is a different wrong answer
// when it goes: numeric runs compared as text puts ".10" before ".2"; non-digit
// runs compared numerically loses every ordinary id; and two ids that agree as
// far as the shorter one goes have to break the tie on length or a parent sorts
// after its own child.
func TestNaturalLessComparesDigitRunsNumerically(t *testing.T) {
	for _, testcase := range []struct {
		a, b string
		want bool
		why  string
	}{
		{"dw32p.2", "dw32p.10", true, "a digit run compares numerically, not as text"},
		{"dw32p.10", "dw32p.2", false, "and the same the other way round"},
		{"dw32p.9", "dw32p.9", false, "equal ids are not less than each other"},
		{"beacon-0bl", "br-6vf", true, "a non-digit run compares as text"},
		{"br-6vf", "beacon-0bl", false, "and the same the other way round"},
		{"dw32p", "dw32p.1", true, "a parent precedes its own child on length"},
		{"dw32p.1", "dw32p", false, "and the child does not precede the parent"},
		{"ab12", "ab12cd", true, "runs equal as far as the shorter goes break the tie on length"},
		{"ab12cd", "ab12", false, "and the longer does not come first"},
		{"k3f9x", "k3f9x", false, "an id with no digit run at all still compares"},
		{"beacon-ci-never-executed-4bhg", "beacon-ci-never-executed-4bhh", true, "a beads-era slug orders on its tail"},
		{"a2b", "a10b", true, "a digit run in the middle is still numeric"},
	} {
		if got := naturalLess(testcase.a, testcase.b); got != testcase.want {
			t.Errorf("naturalLess(%q, %q) = %v, want %v: %s",
				testcase.a, testcase.b, got, testcase.want, testcase.why)
		}
	}
}

// TestIDRunsAlternateDigitsAndText: the split the comparator rests on. An id is
// opaque, so this is the ONE place that looks inside one, and it looks only to
// order it — never to read a project out of it.
func TestIDRunsAlternateDigitsAndText(t *testing.T) {
	for _, testcase := range []struct {
		in   string
		want []string
	}{
		{"dw32p.10", []string{"dw", "32", "p.", "10"}},
		{"k3f9x", []string{"k", "3", "f", "9", "x"}},
		{"mem-2c29x", []string{"mem-", "2", "c", "29", "x"}},
		{"", nil},
		{"1234", []string{"1234"}},
		{"abcd", []string{"abcd"}},
	} {
		got := idRuns(testcase.in)
		if len(got) != len(testcase.want) {
			t.Errorf("idRuns(%q) = %#v, want %#v", testcase.in, got, testcase.want)
			continue
		}
		for i := range got {
			if got[i] != testcase.want[i] {
				t.Errorf("idRuns(%q) = %#v, want %#v", testcase.in, got, testcase.want)
				break
			}
		}
	}
}
