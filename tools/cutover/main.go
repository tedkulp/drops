// Command cutover converts this machine's v6 drops store to v7, in place.
//
// It is the one-time job dw32p.28 exists to perform, and it runs once per
// machine: this repository is cloned on both, and each converts its own
// ~/.drops/drops.db. That is why nothing here is hardcoded to one store's row
// counts — the two databases hold different data, and the recipe has to be true
// of both.
//
// What it does, in order:
//
//  1. Censuses the v6 store independently of the conversion, and refuses a
//     store whose parentage dw32p.12 says is ambiguous.
//  2. Copies the store with VACUUM INTO, proves the copy by re-counting it, and
//     moves the v6 mirror aside.
//  3. Converts in place through store.Rewrite, with the census as Expect, so a
//     conversion that disagrees with the census commits nothing.
//  4. Writes the replica sidecar under the key it converted with.
//  5. Reopens the v7 store and checks foreign keys and all three FTS indexes.
//
// Usage:
//
//	just cutover --dry-run     # convert a throwaway copy and report
//	just cutover               # convert ~/.drops/drops.db for real
package main

import (
	"context"
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
	"github.com/tedkulp/drops/internal/sync"
)

// Exit codes follow the CLI's stable contract: 1 is a failure, 2 is being
// called wrong.
const (
	exitFailed  = 1
	exitMisused = 2
)

func main() {
	set := flag.NewFlagSet("cutover", flag.ContinueOnError)
	var (
		storePath = set.String("store", "", "v6 store to convert (default: the store drops would open)")
		backupDir = set.String("backup-dir", "", "where the backup is written (default: beside the store)")
		dryRun    = set.Bool("dry-run", false, "convert a throwaway copy and report, touching nothing")
		asJSON    = set.Bool("json", false, "report as JSON")
	)
	if err := set.Parse(os.Args[1:]); err != nil {
		os.Exit(exitMisused)
	}
	if set.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "cutover takes no positional arguments, got %q\n", strings.Join(set.Args(), " "))
		os.Exit(exitMisused)
	}

	if err := run(context.Background(), options{
		store:     *storePath,
		backupDir: *backupDir,
		dryRun:    *dryRun,
		asJSON:    *asJSON,
		out:       os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "cutover: %v\n", err)
		os.Exit(exitFailed)
	}
}

type options struct {
	store     string
	backupDir string
	dryRun    bool
	asJSON    bool
	out       *os.File
}

// Outcome is one run's whole account of itself.
type Outcome struct {
	Store    string             `json:"store"`
	DryRun   bool               `json:"dry_run"`
	Backup   string             `json:"backup,omitempty"`
	Replica  model.ReplicaKey   `json:"replica_key"`
	Counts   store.LegacyCounts `json:"counts"`
	Omitted  []model.ID         `json:"omitted_parents,omitempty"`
	Memories []model.ID         `json:"omitted_memories,omitempty"`
	Skipped  []string           `json:"skipped_remotes,omitempty"`
	Checked  []string           `json:"checked"`
}

func run(ctx context.Context, opts options) error {
	path := opts.store
	if path == "" {
		resolved, err := store.DefaultPath()
		if err != nil {
			return err
		}
		path = resolved
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	census, err := TakeCensus(ctx, path)
	if err != nil {
		return err
	}
	// dw32p.12 admits dotted-only parentage only within one Project. A live
	// child whose inferred parent sits elsewhere is evidence of two different
	// things and the rule refuses to choose between them.
	if len(census.AmbiguousParents) > 0 {
		return fmt.Errorf("%d dotted children name a parent in another Project and are not tombstoned: %v",
			len(census.AmbiguousParents), census.AmbiguousParents)
	}

	plan, err := buildPlan(census)
	if err != nil {
		return err
	}

	target, backup := path, ""
	if opts.dryRun {
		scratch, err := os.MkdirTemp("", "drops-cutover-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(scratch)
		target = filepath.Join(scratch, filepath.Base(path))
		if err := copyStore(ctx, path, target); err != nil {
			return err
		}
	} else {
		dir := opts.backupDir
		if dir == "" {
			dir = filepath.Join(filepath.Dir(path), "pre-cutover-"+time.Now().UTC().Format("20060102T150405Z"))
		}
		if backup, err = Backup(ctx, path, dir, census); err != nil {
			return err
		}
	}

	report, err := store.Rewrite(ctx, target, plan)
	if err != nil {
		return fmt.Errorf("convert %s: %w", target, err)
	}

	// The sidecar carries this installation's authoring identity, and SQLite
	// never does. Leaving it to be minted later would author future writes
	// under a key that no converted record names as its origin.
	//
	// A dry run is not special-cased here. It converts a copy in a scratch
	// directory that is deleted on the way out, so the sidecar it writes goes
	// with it — and a `if !opts.dryRun` guard around this was measured unable
	// to fail: no test could observe the difference, because the only
	// directory it changed was the one being discarded.
	sidecar := sync.Sidecar{Replica: plan.Replica}
	if err := sidecar.Save(filepath.Dir(target)); err != nil {
		return fmt.Errorf("write the replica sidecar: %w", err)
	}

	checked, err := verify(ctx, target)
	if err != nil {
		return err
	}

	return emit(opts, Outcome{
		Store:    target,
		DryRun:   opts.dryRun,
		Backup:   backup,
		Replica:  plan.Replica,
		Counts:   report.LegacyCounts,
		Omitted:  report.OmittedParents,
		Memories: report.OmittedMemories,
		Skipped:  report.SkippedRemotes,
		Checked:  checked,
	})
}

// buildPlan turns the census into the decisions store.Rewrite cannot derive:
// which Project key each slug takes, which locator each remote becomes, what
// happens to each memory, and what the outcome must be.
func buildPlan(census Census) (store.LegacyPlan, error) {
	replica, err := model.NewReplicaKey()
	if err != nil {
		return store.LegacyPlan{}, err
	}
	keys, err := ProjectKeys(census.Slugs)
	if err != nil {
		return store.LegacyPlan{}, err
	}
	expect := census.Counts
	return store.LegacyPlan{
		Replica:  replica,
		Projects: keys,
		Locators: Locators(census.Remotes),
		Memories: Curate,
		Expect:   &expect,
	}, nil
}

// verify reopens the converted store through the ordinary v7 path and runs the
// checks the conversion's own transaction could not: the reopen itself, the
// foreign keys, and every FTS index.
//
// dw32p.19 found the plain FTS check unable to fail in one direction, so this
// uses store.CheckFTS, which is the rank-1 form.
func verify(ctx context.Context, path string) ([]string, error) {
	opened, err := store.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reopen the converted store: %w", err)
	}
	defer opened.Close()

	checked := []string{"open"}
	violations, err := opened.ForeignKeyCheck(ctx)
	if err != nil {
		return nil, fmt.Errorf("foreign key check: %w", err)
	}
	if len(violations) > 0 {
		return nil, fmt.Errorf("converted store has %d foreign key violations: %v", len(violations), violations)
	}
	checked = append(checked, "foreign_keys")

	for _, index := range store.FTSIndexes() {
		if err := opened.CheckFTS(ctx, index); err != nil {
			return nil, fmt.Errorf("%s: %w", index, err)
		}
		checked = append(checked, index)
	}
	return checked, nil
}

func emit(opts options, outcome Outcome) error {
	if opts.asJSON {
		raw, err := json.Marshal(outcome)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(opts.out, "%s\n", raw)
		return err
	}

	what := "converted"
	if outcome.DryRun {
		what = "rehearsed"
	}
	fmt.Fprintf(opts.out, "%s %s\n", what, outcome.Store)
	if outcome.Backup != "" {
		fmt.Fprintf(opts.out, "  backup      %s (proved)\n", outcome.Backup)
	}
	fmt.Fprintf(opts.out, "  replica     %s\n", outcome.Replica)
	counts := outcome.Counts
	fmt.Fprintf(opts.out, "  projects    %d\n", counts.Projects)
	fmt.Fprintf(opts.out, "  issues      %d\n", counts.Issues)
	fmt.Fprintf(opts.out, "  parents     %d retained, %d omitted\n", counts.IssueParents, counts.OmittedParents)
	fmt.Fprintf(opts.out, "  deps        %d\n", counts.Dependencies)
	fmt.Fprintf(opts.out, "  labels      %d\n", counts.Labels)
	fmt.Fprintf(opts.out, "  comments    %d\n", counts.Comments)
	fmt.Fprintf(opts.out, "  memories    %d migrated, %d omitted\n", counts.Memories, counts.OmittedMemories)
	if len(outcome.Skipped) > 0 {
		fmt.Fprintf(opts.out, "  no locator  %s\n", strings.Join(outcome.Skipped, ", "))
	}
	fmt.Fprintf(opts.out, "  checked     %s\n", strings.Join(outcome.Checked, ", "))
	if outcome.DryRun {
		fmt.Fprintf(opts.out, "\nnothing was written; the copy has been discarded\n")
	}
	return nil
}
