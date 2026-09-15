// PROTOTYPE — THROWAWAY, branch prototype/board-look only (tedkulp/drops#2).
//
// Seeds a scratch store named so nobody mistakes it for a real one, then runs
// the board layout prototype over it:
//
//	just board-prototype                     # full screen; [ ] d < > to compare
//	just board-prototype -dump 120x40 -variant all -density 3
//	just board-prototype -reseed -closed 1000
//
// The real ~/.drops/drops.db is never opened.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
	"github.com/tedkulp/drops/internal/tui"
)

type protoClock struct{ now time.Time }

func (c *protoClock) Now() time.Time { return c.now }

func main() {
	db := flag.String("db", ".scratch/PROTOTYPE-board-wipe-me.db", "scratch store (created and seeded when absent)")
	reseed := flag.Bool("reseed", false, "wipe the scratch store and seed it again")
	closed := flag.Int("closed", 300, "closed issues to seed")
	variant := flag.String("variant", "A", "layout variant A–E, or `all` with -dump")
	dump := flag.String("dump", "", "print frames as plain text instead of running, e.g. 120x40")
	density := flag.Int("density", 2, "card lines (1–4) for -dump")
	col := flag.Int("col", 0, "focused column (0–3) for -dump")
	sel := flag.Int("sel", 0, "selected card in the focused column for -dump")
	flag.Parse()

	if err := run(*db, *reseed, *closed, *variant, *dump, *density, *col, *sel); err != nil {
		fmt.Fprintln(os.Stderr, "board-prototype:", err)
		os.Exit(1)
	}
}

func run(db string, reseed bool, closed int, variant, dump string, density, col, sel int) error {
	ctx := context.Background()
	if reseed {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(db + suffix)
		}
	}
	_, statErr := os.Stat(db)
	fresh := os.IsNotExist(statErr)
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		return err
	}
	opened, err := store.Open(ctx, db)
	if err != nil {
		return err
	}
	defer opened.Close()

	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		return err
	}
	clock := &protoClock{now: time.Now()}
	issues := core.New(opened, replica, clock, nil, func(core.Warning) {})
	if err := issues.Bootstrap(ctx); err != nil {
		return err
	}
	if fresh {
		if err := seed(ctx, issues, clock, closed); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	project, err := issues.ProjectBySlug(ctx, "drops")
	if err != nil {
		return err
	}

	if dump == "" {
		return tui.RunBoardPrototype(ctx, issues, project, variant)
	}
	var width, height int
	if _, err := fmt.Sscanf(dump, "%dx%d", &width, &height); err != nil {
		return fmt.Errorf("-dump wants WIDTHxHEIGHT: %w", err)
	}
	variants := []string{variant}
	if variant == "all" {
		variants = []string{"A", "B", "C", "D", "E"}
	}
	for _, v := range variants {
		frame, err := tui.DumpBoardPrototype(ctx, issues, project, width, height, v, density, col, sel)
		if err != nil {
			return err
		}
		fmt.Printf("=== variant %s at %dx%d, %d-line cards\n%s\n\n", v, width, height, density, frame)
	}
	return nil
}

type spec struct {
	title  string
	kind   model.IssueType
	prio   int
	labels []string
	who    string
}

var live = []spec{
	/* 0 */ {"Bulk label read for the board's cards", model.TypeTask, 1, []string{"core", "board"}, ""},
	/* 1 */ {"Transactional start-and-claim write for moving a card into In progress", model.TypeFeature, 1, []string{"core", "board"}, ""},
	/* 2 */ {"Board mode in drops tui: columns, cards, and the detail pane", model.TypeEpic, 1, []string{"tui", "board"}, ""},
	/* 3 */ {"tab and shift+tab move the card cursor between board columns", model.TypeTask, 2, []string{"tui", "board"}, ""},
	/* 4 */ {"m opens the move picker over open / in progress / closed", model.TypeTask, 2, []string{"tui", "board"}, ""},
	/* 5 */ {"Document the board in cli-contract's TUI section", model.TypeChore, 2, []string{"docs", "board"}, ""},
	/* 6 */ {"Closed column loads every closed issue in scope; measure before bounding", model.TypeResearch, 2, []string{"perf", "board"}, ""},
	/* 7 */ {"sync import bumps write_seq on a tombstone replay it should skip", model.TypeBug, 1, []string{"sync"}, ""},
	/* 8 */ {"doctor --repair leaves an orphaned locator after a project rename", model.TypeBug, 2, []string{"doctor"}, "codex"},
	/* 9 */ {"Colour, --no-color and NO_COLOR for the navigator", model.TypeDecision, 3, []string{"tui"}, ""},
	/* 10 */ {"Mutation control for the ready-only predicate under a filtered scope", model.TypeTask, 2, []string{"mutation", "tui"}, ""},
	/* 11 */ {"Shell completion offers tombstoned ids for close", model.TypeBug, 3, []string{"cli"}, ""},
	/* 12 */ {"A title long enough that it will not fit on any card at any width people actually run their terminals at, which is rather the point of it", model.TypeTask, 3, []string{"tui", "board", "core", "sync", "docs", "perf"}, ""},
	/* 13 */ {"drops export --since for incremental mirror pushes", model.TypeFeature, 3, []string{"mirror", "sync"}, ""},
	/* 14 */ {"Flaky lockfile contention test under -race on a loaded CI runner", model.TypeBug, 1, []string{"ci", "lockfile"}, ""},
	/* 15 */ {"Memory rows show the wrong project slug under --all", model.TypeBug, 2, []string{"memory"}, ""},
	/* 16 */ {"Show deferred-until on the detail page header", model.TypeTask, 4, nil, ""},
	/* 17 */ {"Rename the x Actions rows to match the CLI verbs", model.TypeChore, 4, []string{"tui"}, ""},
	/* 18 */ {"Investigate WAL checkpoint stalls on a 10k-issue store", model.TypeResearch, 2, []string{"perf", "store"}, ""},
	/* 19 */ {"Grouping modes for the board (priority / type columns)", model.TypeFeature, 4, []string{"board"}, ""},
	/* 20 */ {"Label rename across the store in one revision", model.TypeFeature, 3, []string{"core"}, ""},
	/* 21 */ {"Refuse dep add that would close a cycle, naming the path", model.TypeTask, 2, []string{"core"}, ""},
	// In progress from here.
	/* 22 */ {"Bump schema to v8 and write the migration test first", model.TypeTask, 1, []string{"store"}, "Ted Kulp"},
	/* 23 */ {"GoReleaser: sign checksums with cosign", model.TypeChore, 2, []string{"release"}, "codex"},
	/* 24 */ {"Stale marker survives R when a sync lands mid-refresh", model.TypeBug, 1, []string{"tui", "sync"}, "Ted Kulp"},
	/* 25 */ {"Board card layout prototype", model.TypeResearch, 1, []string{"board"}, "claude"},
	/* 26 */ {"Detail pane horizontal scroll loses its offset on resize", model.TypeBug, 3, []string{"tui"}, ""},
	/* 27 */ {"Split cli-contract.md by verb group", model.TypeChore, 3, []string{"docs"}, "codex"},
}

const firstInProgress = 22

// blocks[i] = {blocked, blocker}.
var blocks = [][2]int{
	{2, 0}, {2, 1}, {4, 1}, {5, 2}, {5, 4}, {19, 2}, {6, 0}, {3, 25}, {21, 20}, {13, 7},
	{22, 18}, {23, 14}, {24, 7},
}

var verbs = []string{"Fix", "Measure", "Document", "Refactor", "Decide", "Test", "Remove", "Rename", "Guard", "Split"}
var nouns = []string{
	"the lockfile journal restore", "render.Ellipsis on combining marks", "sync merge of concurrent label writes",
	"the list verb's --json shape", "project resolution from a worktree", "memory search ranking",
	"the census test's registry", "doctor's credential scan", "the picker's header scroll", "reopen's y/N prompt",
	"write_seq bumps in import", "the follow stack under a filter", "claim and release in the Actions modal",
	"GoReleaser's archive names", "the mutate harness restore path", "store.Open's busy timeout",
}

func seed(ctx context.Context, issues *core.Core, clock *protoClock, closedCount int) error {
	r := rand.New(rand.NewPCG(2, 15))
	now := clock.now
	at := func(daysAgo float64) { clock.now = now.Add(-time.Duration(daysAgo * float64(24*time.Hour))) }

	at(500)
	project, err := issues.CreateProject(ctx, "drops")
	if err != nil {
		return err
	}
	other, err := issues.CreateProject(ctx, "beacon")
	if err != nil {
		return err
	}
	create := func(key model.ProjectKey, s spec, daysAgo float64) (model.ID, error) {
		at(daysAgo)
		input := core.CreateIssue{Project: key, Title: s.title, Type: s.kind, Priority: s.prio, Labels: s.labels}
		if s.who != "" {
			who := s.who
			input.Assignee = &who
		}
		issue, err := issues.CreateIssue(ctx, input)
		return issue.ID, err
	}

	// Writes are replayed oldest first: core never lets a timestamp run
	// backwards, so backdated writes out of order all collapse onto the latest.
	type event struct {
		daysAgo float64
		do      func() error
	}
	var events []event
	ids := make([]model.ID, len(live))
	for i, s := range live {
		created := 1 + r.Float64()*60
		events = append(events, event{created, func() (err error) {
			ids[i], err = create(project.Key, s, created)
			return err
		}})
		if i >= firstInProgress {
			started := created * r.Float64()
			events = append(events, event{started, func() error {
				at(started)
				_, err := issues.SetIssueStatus(ctx, ids[i], model.StatusInProgress, "")
				return err
			}})
		}
	}
	for i := 0; i < closedCount; i++ {
		s := spec{
			title: verbs[r.IntN(len(verbs))] + " " + nouns[r.IntN(len(nouns))],
			kind:  []model.IssueType{model.TypeTask, model.TypeBug, model.TypeChore, model.TypeFeature}[r.IntN(4)],
			prio:  r.IntN(5),
		}
		if r.IntN(3) == 0 {
			s.labels = []string{[]string{"core", "tui", "sync", "cli", "docs"}[r.IntN(5)]}
		}
		if r.IntN(2) == 0 {
			s.who = []string{"Ted Kulp", "codex", "claude"}[r.IntN(3)]
		}
		created := 2 + r.Float64()*400
		closedAgo := created * r.Float64() * r.Float64()
		var id model.ID
		events = append(events,
			event{created, func() (err error) {
				id, err = create(project.Key, s, created)
				return err
			}},
			event{closedAgo, func() error {
				at(closedAgo)
				_, err := issues.SetIssueStatus(ctx, id, model.StatusClosed, "Done — PROTOTYPE seed")
				return err
			}})
	}
	for _, title := range []string{"Beacon onboarding flow", "Beacon rate limiter", "Beacon docs pass"} {
		events = append(events, event{3, func() error {
			_, err := create(other.Key, spec{title: title, kind: model.TypeTask, prio: 2}, 3)
			return err
		}})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].daysAgo > events[j].daysAgo })
	for _, e := range events {
		if err := e.do(); err != nil {
			return err
		}
	}

	at(0)
	for _, edge := range blocks {
		if _, err := issues.SetDependency(ctx, ids[edge[0]], ids[edge[1]], model.DepBlocks, true); err != nil {
			return err
		}
	}
	return nil
}
