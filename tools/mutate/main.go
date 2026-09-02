// Command mutate proves that this repository's tests can fail.
//
// AGENTS.md holds the standard: each requirement clause has a control — the
// production branch that makes it true — and that control has been mutated with
// its test observed red. The standard is not a coverage number, because
// coverage cannot see the defect it exists to catch: internal/render and
// internal/resolve both sit at 100% function coverage and mutation found real
// defects in both.
//
// The four steps AGENTS.md used to spell out by hand are what this command
// does, per control:
//
//  1. Snapshot the file and record its checksum.
//  2. Apply the mutation.
//  3. Run the one named test, expecting failure. A pass is the finding.
//  4. Restore from the snapshot and verify the checksum.
//
// The snapshot is harness-owned rather than `git checkout`, because a restore
// command cannot distinguish a mutation from uncommitted work. A run
// interrupted between step 2 and step 4 leaves a journal naming exactly what to
// put back; `mutate --restore` completes it.
//
// Usage:
//
//	go run ./tools/mutate                    # every catalogued control
//	go run ./tools/mutate render resolve     # by package or by control name
//	go run ./tools/mutate --list             # what is catalogued, without running
//	go run ./tools/mutate --restore          # finish an interrupted run
//	go run ./tools/mutate --file F --old A --new B --test T --clause C
//
// Controls live beside the code they mutate, in each package's mutations.json.
package main

import (
	"context"
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Exit codes. Distinct from drops' own contract — this is development tooling,
// not the CLI — but the same principle: a caller can tell the cases apart.
const (
	exitOK      = 0 // every selected control went red
	exitFinding = 1 // at least one test cannot fail for the clause it covers
	exitUsage   = 2 // called wrong, or a catalogue that does not load
	exitUnclean = 3 // a restore failed: the working tree may still hold a mutation
)

func main() {
	os.Exit(run())
}

// parseArgs permutes flags and positional arguments so `mutate memory --json`
// works. Go's flag package stops at the first non-flag argument, which would
// silently turn a trailing --json into a control filter that matches nothing;
// re-parsing what follows each positional accepts them in any order.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func run() int {
	fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
	var (
		list    = fs.Bool("list", false, "print the catalogued controls and exit")
		restore = fs.Bool("restore", false, "restore a mutation left by an interrupted run")
		race    = fs.Bool("race", false, "run the tests under -race")
		asJSON  = fs.Bool("json", false, "emit the record as JSON")
		file    = fs.String("file", "", "ad-hoc: the file to mutate")
		old     = fs.String("old", "", "ad-hoc: the text to replace, which must appear exactly once")
		neu     = fs.String("new", "", "ad-hoc: what to replace it with")
		test    = fs.String("test", "", "ad-hoc: the test that must go red")
		clause  = fs.String("clause", "", "ad-hoc: the requirement clause this control proves")
	)
	filters, err := parseArgs(fs, os.Args[1:])
	if err != nil {
		return exitUsage
	}

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutate:", err)
		return exitUsage
	}
	snapDir := filepath.Join(root, ".scratch", "mutate")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := &Runner{
		Root:    root,
		SnapDir: snapDir,
		RunTest: goTest(ctx, root, *race),
		Out:     &Report{W: os.Stdout},
	}

	if *restore {
		f, err := r.Recover()
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "mutate:", err)
			return exitUnclean
		case f == "":
			fmt.Println("nothing to restore")
		default:
			fmt.Printf("restored %s\n", f)
		}
		return exitOK
	}

	// An interrupted run left a mutation in the tree. Refusing here is the
	// point: a second run would snapshot the *mutated* file and then "restore"
	// the mutation as if it were the author's own work.
	if s, ok := r.Pending(); ok {
		fmt.Fprintf(os.Stderr, "mutate: an interrupted run left %s mutated.\n", s.File)
		fmt.Fprintln(os.Stderr, "        run `just mutate --restore` before anything else.")
		return exitUnclean
	}

	controls, code := gather(root, filters, *file, *old, *neu, *test, *clause)
	if code != exitOK {
		return code
	}

	if len(controls) == 0 {
		fmt.Fprintln(os.Stderr, "mutate: no controls are catalogued yet; see AGENTS.md")
		return exitUsage
	}
	if *list {
		for _, c := range controls {
			fmt.Printf("%-24s %-34s %s\n", c.pkg(), c.Name, c.Test)
		}
		return exitOK
	}

	// Restore whatever is in flight if the run is interrupted, so Ctrl-C during
	// a long run does not hand back a mutated tree.
	go func() {
		<-ctx.Done()
		r.RestoreActive()
	}()

	if *asJSON {
		r.Out = nil
	}
	results := r.Run(controls)

	if *asJSON {
		b, err := json.Marshal(results, json.FormatNilSliceAsNull(false))
		if err != nil {
			fmt.Fprintln(os.Stderr, "mutate:", err)
			return exitUsage
		}
		fmt.Printf("%s\n", b)
	}

	clean := true
	for _, res := range results {
		if res.Sum != "" && !res.Restored {
			clean = false
		}
	}
	if !clean {
		fmt.Fprintln(os.Stderr, "mutate: a restore failed — the working tree may still hold a mutation")
		return exitUnclean
	}

	proved := true
	if !*asJSON {
		proved = r.Out.Summary(results)
	} else {
		for _, res := range results {
			if !res.Outcome.ok() {
				proved = false
			}
		}
	}
	if !proved {
		return exitFinding
	}
	return exitOK
}

// gather builds the control list from either the catalogues or the ad-hoc
// flags. The two are exclusive: an ad-hoc control is an exploration, and mixing
// it into a catalogue run would put an unrecorded control in the record.
func gather(root string, filters []string, file, old, neu, test, clause string) ([]Control, int) {
	adhoc := file != "" || old != "" || test != ""
	if adhoc {
		if len(filters) > 0 {
			fmt.Fprintln(os.Stderr, "mutate: --file takes no positional filters")
			return nil, exitUsage
		}
		for _, f := range []struct{ name, value string }{
			{"--file", file}, {"--old", old}, {"--test", test}, {"--clause", clause},
		} {
			if f.value == "" {
				fmt.Fprintf(os.Stderr, "mutate: %s is required for an ad-hoc control\n", f.name)
				return nil, exitUsage
			}
		}
		return []Control{{
			Name: "ad-hoc", Clause: clause, File: file, Test: test, Old: old, New: neu,
		}}, exitOK
	}

	controls, err := discoverControls(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutate:", err)
		return nil, exitUsage
	}
	controls, err = selectControls(controls, filters)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutate:", err)
		return nil, exitUsage
	}
	return controls, exitOK
}

// repoRoot walks up for go.mod. Every control's file is repo-relative, so the
// harness must know where the repository starts regardless of where it was run.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
