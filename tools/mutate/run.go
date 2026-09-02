package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Outcome is what one control's run proved.
//
// Only Red is a pass. Every other value is a *finding* — something the record
// must say out loud — because the harness exists to catch the case AGENTS.md
// names as this codebase's dominant defect: a test that appears to cover a
// requirement but cannot fail for it.
type Outcome string

const (
	Red         Outcome = "RED"          // the test failed under mutation: the control is proved
	Green       Outcome = "GREEN"        // the test passed under mutation: it does not cover the clause
	NotRun      Outcome = "NOT RUN"      // no such test — `go test` reports ok and exits 0 for this
	Skipped     Outcome = "SKIPPED"      // the test skipped, which is not a failure and not proof
	BuildFailed Outcome = "BUILD FAILED" // the mutation does not compile, so it proves nothing
	NoMatch     Outcome = "NO MATCH"     // `old` is absent or repeated: the control has drifted
	BaselineRed Outcome = "BASELINE RED" // the test was already failing, so its red says nothing
	Broken      Outcome = "ERROR"        // the harness could not complete the control
)

// ok reports whether an outcome is the one the discipline asks for.
func (o Outcome) ok() bool { return o == Red }

// Result is the record of one control, and is what a ticket's close reason is
// assembled from.
type Result struct {
	Control  Control       `json:"control"`
	Outcome  Outcome       `json:"outcome"`
	Detail   string        `json:"detail,omitempty"`
	Sum      string        `json:"sha256,omitempty"` // the pre-mutation checksum
	Restored bool          `json:"restored"`         // the file was put back and re-checked against Sum
	Elapsed  time.Duration `json:"-"`
	// encoding/json/v2 refuses a time.Duration outright — "no default
	// representation" — rather than guessing between nanoseconds and a string,
	// so the wire form is stated here instead of inherited.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// testFunc runs one named test in one package and reports what happened.
type testFunc func(pkg, test string) (Verdict, error)

// Runner executes controls. Everything it learns about its environment is a
// field — the repository root, where snapshots go, how a test is run — so its
// orchestration is exercised without compiling anything, per the seam rule in
// AGENTS.md.
type Runner struct {
	Root     string   // repository root; every control's file is relative to it
	SnapDir  string   // harness-owned snapshot store
	RunTest  testFunc //
	Out      *Report  //
	baseline map[string]Verdict

	mu     sync.Mutex
	active *snapshot // the file currently mutated, for the signal handler
}

// journalPath is where an in-flight mutation is recorded, so a harness killed
// between the mutation and the restore leaves evidence rather than a silently
// edited tree.
func (r *Runner) journalPath() string { return filepath.Join(r.SnapDir, "journal.json") }

// Recover restores a mutation left behind by an interrupted run. It is what
// makes the crash case survivable: the journal names the file, the snapshot and
// the checksum, so the restore is proved exactly as it is in the normal path.
func (r *Runner) Recover() (string, error) {
	b, err := os.ReadFile(r.journalPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var s snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return "", fmt.Errorf("%s is unreadable; the file it names must be restored by hand: %w", r.journalPath(), err)
	}
	if err := s.restore(); err != nil {
		return s.File, err
	}
	_ = s.discard()
	return s.File, os.Remove(r.journalPath())
}

// Pending reports the file an interrupted run left mutated, if any.
func (r *Runner) Pending() (snapshot, bool) {
	b, err := os.ReadFile(r.journalPath())
	if err != nil {
		return snapshot{}, false
	}
	var s snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return snapshot{}, false
	}
	return s, true
}

// RestoreActive puts back whatever mutation is in flight. The signal handler
// calls it; it is a no-op when nothing is mutated.
func (r *Runner) RestoreActive() {
	r.mu.Lock()
	s := r.active
	r.active = nil
	r.mu.Unlock()
	if s == nil {
		return
	}
	if err := s.restore(); err != nil {
		fmt.Fprintf(os.Stderr, "mutate: %v\n", err)
		return
	}
	_ = s.discard()
	_ = os.Remove(r.journalPath())
}

// Run executes every control in order and returns their results.
func (r *Runner) Run(controls []Control) []Result {
	if r.baseline == nil {
		r.baseline = map[string]Verdict{}
	}
	results := make([]Result, 0, len(controls))
	for _, c := range controls {
		res := r.one(c)
		results = append(results, res)
		if r.Out != nil {
			r.Out.Control(res)
		}
	}
	return results
}

func (r *Runner) one(c Control) Result {
	start := time.Now()
	res := Result{Control: c}
	done := func(o Outcome, detail string) Result {
		res.Outcome, res.Detail = o, detail
		res.Elapsed = time.Since(start)
		res.ElapsedMS = res.Elapsed.Milliseconds()
		return res
	}

	// The test must pass before it can mean anything by failing. A test already
	// red goes red under any mutation, so recording it as proof would be the
	// defect the harness exists to find, dressed as a success.
	base, err := r.baselineFor(c)
	if err != nil {
		return done(Broken, err.Error())
	}
	switch {
	case base.BuildFailed:
		return done(Broken, "the package does not compile before any mutation:\n"+base.BuildOutput)
	case !base.Ran:
		return done(NotRun, fmt.Sprintf("no test named %s ran in %s — `go test -run` matching nothing reports ok and exits 0", c.Test, c.pkg()))
	case base.Skipped:
		return done(Skipped, c.Test+" skips before any mutation, so it can never go red")
	case base.Failed:
		return done(BaselineRed, c.Test+" already fails without the mutation")
	}

	file := filepath.Join(r.Root, c.File)
	snap, err := takeSnapshot(r.SnapDir, file)
	if err != nil {
		return done(Broken, err.Error())
	}
	res.Sum = snap.Sum

	if err := writeJournal(r.journalPath(), snap); err != nil {
		_ = snap.discard()
		return done(Broken, err.Error())
	}

	if err := applyMutation(file, c.Old, c.New); err != nil {
		// The file was never written, so there is nothing to restore, and the
		// record should say that rather than claim a restore it did not make.
		_ = snap.discard()
		_ = os.Remove(r.journalPath())
		res.Sum, res.Restored = "", true
		var occ *occurrenceError
		if errors.As(err, &occ) {
			// Name the file as the catalogue spells it: an absolute path is
			// noise in a record that is read next to the catalogue.
			occ.File = c.File
			return done(NoMatch, occ.Error())
		}
		return done(Broken, err.Error())
	}

	r.mu.Lock()
	r.active = &snap
	r.mu.Unlock()

	v, runErr := r.RunTest(c.pkg(), c.Test)

	r.mu.Lock()
	r.active = nil
	r.mu.Unlock()

	if err := snap.restore(); err != nil {
		// Loud and terminal: the tree may still hold the mutation.
		return done(Broken, err.Error())
	}
	res.Restored = true
	_ = snap.discard()
	_ = os.Remove(r.journalPath())

	if runErr != nil {
		return done(Broken, runErr.Error())
	}
	switch {
	case v.BuildFailed:
		return done(BuildFailed, "the mutation does not compile, so nothing was proved:\n"+strings.TrimSpace(v.BuildOutput))
	case !v.Ran:
		return done(NotRun, "no test named "+c.Test+" ran under the mutation")
	case v.Skipped:
		return done(Skipped, c.Test+" skipped under the mutation")
	case v.Failed:
		return done(Red, strings.TrimSpace(firstLines(dropRunLines(v.Output), 3)))
	default:
		return done(Green, c.Test+" passed with the branch broken, so it does not cover this clause")
	}
}

func (r *Runner) baselineFor(c Control) (Verdict, error) {
	key := c.pkg() + "\x00" + c.Test
	if v, ok := r.baseline[key]; ok {
		return v, nil
	}
	v, err := r.RunTest(c.pkg(), c.Test)
	if err != nil {
		return Verdict{}, err
	}
	r.baseline[key] = v
	return v, nil
}

// dropRunLines removes go test's "=== RUN" bookkeeping. What a record wants
// from a failure is the assertion that fired, not the transcript around it.
func dropRunLines(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "=== ") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// firstLines keeps a failure's opening lines; a whole Go test failure is often
// hundreds of lines of diff and the record wants the claim, not the transcript.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

// noDefaultStore mirrors the justfile's tripwire. `go test` inherits this
// process's environment, so a harness run with DROPS_DB unset would disarm the
// one thing stopping a test from reaching ~/.drops/drops.db. /dev/null is not a
// directory, so store.Open's MkdirAll fails with ENOTDIR and names the path.
// The justfile exports its own copy for `just mutate`; this is the fallback
// that makes a bare `go run ./tools/mutate` equally safe.
const noDefaultStore = "/dev/null/no-default-store-in-tests/drops.db"

// goTest runs one named test and reads the answer out of the event stream
// rather than the exit code, because the exit code is one of the three things
// AGENTS.md records as lying about success here.
func goTest(ctx context.Context, root string, race bool) testFunc {
	return func(pkg, test string) (Verdict, error) {
		args := []string{"test", "-json", "-count=1", "-run", runPattern(test)}
		if race {
			args = append(args, "-race")
		}
		args = append(args, pkg)

		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = root
		cmd.Env = os.Environ()
		if os.Getenv("DROPS_DB") == "" {
			cmd.Env = append(cmd.Env, "DROPS_DB="+noDefaultStore)
		}
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		// A failing test is the expected case, so the exit status is not read.
		_ = cmd.Run()
		return parseVerdict(&out, test)
	}
}

// runPattern turns a test name into a -run argument. go test splits the pattern
// on "/" and matches each element against one level of the test tree, so a
// subtest is anchored element by element; anchoring the whole string would make
// "TestWrap" also select "TestWrapFence".
func runPattern(test string) string {
	parts := strings.Split(test, "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	return strings.Join(parts, "/")
}
