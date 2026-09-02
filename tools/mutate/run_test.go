package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRunner stands in for `go test`. The Runner's job is orchestration —
// baseline, snapshot, mutate, judge, restore — and stating the verdict rather
// than compiling for it is what lets every outcome branch be reached.
type fixtureRunner struct {
	baseline Verdict
	mutated  Verdict
	calls    int
	sawFile  string // the file's contents at the moment the test "ran"
	file     string
}

func (f *fixtureRunner) run(pkg, test string) (Verdict, error) {
	f.calls++
	if f.calls == 1 {
		return f.baseline, nil
	}
	if b, err := os.ReadFile(f.file); err == nil {
		f.sawFile = string(b)
	}
	return f.mutated, nil
}

func newRunner(t *testing.T, f *fixtureRunner) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "p.go")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	f.file = file
	return &Runner{
		Root:    root,
		SnapDir: filepath.Join(root, ".scratch", "mutate"),
		RunTest: f.run,
	}, file
}

var control = Control{
	Name: "less-than-is-strict", Clause: "f reports a strictly less than b",
	File: "p.go", Test: "TestLess", Old: "return a < b", New: "return a <= b",
}

func TestRunClassifiesEachOutcome(t *testing.T) {
	pass := Verdict{Ran: true}
	tests := []struct {
		name     string
		baseline Verdict
		mutated  Verdict
		want     Outcome
	}{
		{"the test fails under the mutation", pass, Verdict{Ran: true, Failed: true}, Red},
		{"the test passes under the mutation", pass, pass, Green},
		{"the mutation does not compile", pass, Verdict{BuildFailed: true, BuildOutput: "boom"}, BuildFailed},
		{"the test skips under the mutation", pass, Verdict{Ran: true, Skipped: true}, Skipped},
		{"no such test exists", Verdict{}, Verdict{}, NotRun},
		{"the test was already failing", Verdict{Ran: true, Failed: true}, Verdict{Ran: true, Failed: true}, BaselineRed},
		{"the test skips before any mutation", Verdict{Ran: true, Skipped: true}, pass, Skipped},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fixtureRunner{baseline: tc.baseline, mutated: tc.mutated}
			r, file := newRunner(t, f)
			got := r.Run([]Control{control})
			if len(got) != 1 {
				t.Fatalf("got %d results", len(got))
			}
			if got[0].Outcome != tc.want {
				t.Errorf("outcome = %q, want %q (detail: %s)", got[0].Outcome, tc.want, got[0].Detail)
			}
			// Whatever the outcome, the file is what it was.
			if b, _ := os.ReadFile(file); string(b) != source {
				t.Errorf("file left modified:\n%q", b)
			}
		})
	}
}

// The mutation has to actually be in the file while the test runs. A harness
// that snapshots, restores and *then* runs would report every control red.
func TestRunMutatesTheFileWhileTheTestRuns(t *testing.T) {
	f := &fixtureRunner{baseline: Verdict{Ran: true}, mutated: Verdict{Ran: true, Failed: true}}
	r, _ := newRunner(t, f)
	r.Run([]Control{control})
	if !strings.Contains(f.sawFile, "return a <= b") {
		t.Errorf("the test ran against unmutated source:\n%q", f.sawFile)
	}
}

// A baseline that already fails must not be mutated at all: the file is never
// touched, and the outcome says why.
func TestBaselineRedNeverMutates(t *testing.T) {
	f := &fixtureRunner{baseline: Verdict{Ran: true, Failed: true}, mutated: Verdict{Ran: true, Failed: true}}
	r, _ := newRunner(t, f)
	res := r.Run([]Control{control})
	if res[0].Outcome != BaselineRed {
		t.Fatalf("outcome = %q, want %q", res[0].Outcome, BaselineRed)
	}
	if f.calls != 1 {
		t.Errorf("ran the test %d times; a failing baseline must stop before the mutation", f.calls)
	}
}

// The baseline is per (package, test), so a package with several controls on
// one test pays for it once.
func TestBaselineIsCachedPerTest(t *testing.T) {
	f := &fixtureRunner{baseline: Verdict{Ran: true}, mutated: Verdict{Ran: true, Failed: true}}
	r, _ := newRunner(t, f)
	second := control
	second.Name = "another"
	second.Old = "return a > b"
	second.New = "return a >= b"
	r.Run([]Control{control, second})
	// 1 baseline + 2 mutation runs, not 2 + 2.
	if f.calls != 3 {
		t.Errorf("ran the test %d times, want 3 (one shared baseline)", f.calls)
	}
}

func TestControlThatDoesNotMatchIsAFinding(t *testing.T) {
	f := &fixtureRunner{baseline: Verdict{Ran: true}, mutated: Verdict{Ran: true, Failed: true}}
	r, file := newRunner(t, f)
	drifted := control
	drifted.Old = "return a >= b"
	res := r.Run([]Control{drifted})
	if res[0].Outcome != NoMatch {
		t.Errorf("outcome = %q, want %q", res[0].Outcome, NoMatch)
	}
	if b, _ := os.ReadFile(file); string(b) != source {
		t.Error("a control that does not match must not touch the file")
	}
}

// The journal exists so an interrupted run is recoverable. It must be gone once
// the file is verified back, or every later run would refuse to start.
func TestJournalIsClearedAfterAVerifiedRestore(t *testing.T) {
	f := &fixtureRunner{baseline: Verdict{Ran: true}, mutated: Verdict{Ran: true, Failed: true}}
	r, _ := newRunner(t, f)
	r.Run([]Control{control})
	if _, ok := r.Pending(); ok {
		t.Error("the journal survived a completed run, so the next run would refuse to start")
	}
}

// The crash case: a journal and a mutated file, with no harness running.
func TestRecoverRestoresAnInterruptedMutation(t *testing.T) {
	f := &fixtureRunner{}
	r, file := newRunner(t, f)
	snap, err := takeSnapshot(r.SnapDir, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(r.journalPath(), snap); err != nil {
		t.Fatal(err)
	}
	if err := applyMutation(file, "return a < b", "return true"); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Pending(); !ok {
		t.Fatal("Pending did not see the journal, so a later run would snapshot the mutated file")
	}
	got, err := r.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got != file {
		t.Errorf("Recover reported %q, want %q", got, file)
	}
	if b, _ := os.ReadFile(file); string(b) != source {
		t.Errorf("Recover left the mutation in place:\n%q", b)
	}
	if _, ok := r.Pending(); ok {
		t.Error("Recover left the journal behind")
	}
}

func TestRunPatternAnchorsEachElement(t *testing.T) {
	tests := []struct{ in, want string }{
		{"TestWrap", "^TestWrap$"},
		{"TestSub/child_two", "^TestSub$/^child_two$"},
		{"TestA/b.c", `^TestA$/^b\.c$`},
	}
	for _, tc := range tests {
		if got := runPattern(tc.in); got != tc.want {
			t.Errorf("runPattern(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
