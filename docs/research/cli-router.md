# Does cobra still earn its place?

Research for `dw32p.1`. Question: in a from-scratch rewrite of `drops` — a Go CLI issue
tracker whose primary users are AI coding agents shelling out to it constantly — does
`spf13/cobra` still earn its place, or does the CLI surface come from `flag`, a smaller
router, or hand-rolled dispatch?

**Answer: cobra stays.** Not because it is free, but because the two things that would
justify replacing it — process startup and line count — are both measured, and both are
noise. The one real defect found (usage errors exit `1` instead of `2`) is a bug in
`ExitCodeFor`, not in cobra, and closing it costs about ten lines plus a mechanical edit
at 57 sites. Every alternative that can parse the CLI contract drops already documents
costs *more* binary and buys nothing measurable.

Everything below about the reference implementation is measured against
`/home/ted/src/drops.orig` at the state of 2026-09-01. Everything about the libraries is
from their source in the module cache, the Go module proxy, or the GitHub releases API.

---

## 1. How much of `internal/cli` is cobra ceremony?

`internal/cli` is 21 production files, **5152 production lines** against 12018 test lines
(`wc -l` over `*.go` excluding `*_test.go`; matches the ticket's figure exactly).

Classified mechanically with a `go/ast` walker rather than by eye. A line is **wiring**
if it is inside a `&cobra.Command{...}` literal (excluding run-hook bodies), a
`.Flags()`/`.PersistentFlags()`/`.AddCommand`/`Set*` call, a `spf13/*` import, or the
signature/closing brace of a `func … *cobra.Command` factory. It is **help** if it is the
value of a prose key (`Use`, `Short`, `Long`, `Example`, `Aliases`) — text a user reads,
which survives any router. Everything else that is not blank or comment is **behaviour**.

| bucket | lines | share |
|---|---:|---:|
| router wiring (cobra-specific) | **666** | 12.9% |
| help/usage prose in command literals (portable) | 324 | 6.3% |
| behaviour | 2617 | 50.8% |
| comments | 1272 | 24.7% |
| blank | 273 | 5.3% |
| **total** | **5152** | |

Per file, the wiring column: `memory.go` 90, `issue.go` 67, `root.go` 66, `sync.go` 63,
`query.go` 60, `dolt.go` 48, `project.go` 45, `dep.go` 40, `config.go` 36, `migrate.go`
33, `comment.go` 32, `label.go` 28, `doctor.go` 23, `move.go` 15, `prime.go` 11,
`version.go` 9. The four pure-rendering files (`showrender.go` 347, `listrender.go` 138,
`pager.go` 100, `jsonout.go` 33 — 618 lines) contain **zero** wiring: they never import
cobra.

Structural census over the same source:

- **61** `&cobra.Command{}` literals, **57** with a `RunE`
- **25** `AddCommand` call sites; 31 visible top-level verbs; nine groups with
  subcommands (`dep` 4, `memory` 3, `project` 4, `label` 3, `comment` 3, `sync` 4,
  `config` 1, `migrate` 3, `dolt` 7). Two levels deep, never three.
- **80** flag registrations: **10** persistent (one block, on root) and 70 local
- **57** `Args:` validators — 28 `NoArgs`, 19 `ExactArgs`, 6 `MinimumNArgs`,
  2 `MaximumNArgs`, 2 `ArbitraryArgs`

That is **666 / 61 ≈ 11 lines of wiring per command**. It is not nothing, but it is not
where the package's mass is: half the file is behaviour and a quarter is comments.

Two corrections to the 5152 baseline that matter for the rewrite:

**The cuts take a chunk of the wiring with them.** The map cuts `migrate`, `dolt` and
`compat`. `migrate.go` (441 lines, 33 wiring), `dolt.go` (158, 48) and `compat_init.go`
(55, 0) go outright, dropping wiring from 666 to **585**. `root.go` loses more: of its
908 lines, `uncoveredOrNoArgs` (107), `newHumanCmd` (26), `helpFlagWasSet` (19),
`isUncoveredVerb` (10) and `shouldReportError` (17) are entirely compat, the compat
pre-parse block inside `NewRootCmd` is ~140 more, and 134 of its 546 comment lines are
about the `bd` face. `root.go` should land near 550.

**105 comment lines exist only to warn about cobra.** Across the package, 1272 lines are
comments; 105 of them name cobra or pflag mechanics (`PersistentPreRunE`, `ParseFlags`,
`ValidateArgs`, `flag.ErrHelp`, `command.go:NNN`), and 84 of those are in `root.go`. This
is the honest hidden cost of cobra here: not the wiring, but the ~2% of the package spent
documenting the order in which cobra does things. Every one of those comments describes a
trap that was actually hit — they are not speculative.

---

## 2. Does cobra's error handling support or fight the exit-code contract?

**It fights it, and the contract is currently broken because of it.**

`README.md` §"Exit codes" documents ten stable codes: 0, 1, 2, 4, 5, 6, 7, 8, 9, 10
(3 exists in `ExitCodeFor` for the `bd` shim's `die_unknown` but is not in the README —
it goes with the compat cut). `ExitCodeFor` (`internal/cli/root.go:89-141`) maps them by
`errors.Is` on service and store sentinels, with `default: return 1`.

Measured against the real binary (`/home/ted/src/drops.orig/drops`, `--db` a scratch
store):

| argv | exit | who produced the error |
|---|---:|---|
| `version` | 0 | — |
| `show nosuchid` | **4** | `service.ErrNotFound` |
| `create --priority 99 x` | **2** | `service.ErrInvalid` |
| `nosuchverb` | **1** | `cobra.NoArgs` — `fmt.Errorf("unknown command %q…")` |
| `--nosuchflag list` | **1** | pflag |
| `list --nosuchflag` | **1** | pflag |
| `--db` (no value) | **1** | pflag |
| `--project` (no value) | **1** | pflag |
| `show` (0 args) | **1** | `cobra.MinimumNArgs` |
| `create` (0 args) | **1** | `cobra.ExactArgs` |
| `dep add onlyone` | **1** | `cobra.ExactArgs` |
| `list --limit notanumber` | **1** | pflag `InvalidValueError` |

The seam between "invalid input" (2) and "unspecified error" (1) falls **exactly where
cobra's ownership begins**. Everything the service rejects gets a real code. Everything
cobra or pflag rejects collapses to 1 — the same code a panic or a disk error would
produce. An agent that typos a flag cannot tell "I called it wrong" from "it broke".

The mechanism, from cobra v1.10.2 source:

- `args.go:36,44` — `NoArgs`/`legacyArgs` return bare `fmt.Errorf`. So do `ExactArgs`,
  `MinimumNArgs`, `MaximumNArgs`, `RangeArgs` (`args.go:69-107`). **No sentinel, no
  type.** `errors.Is`/`errors.As` cannot reach them.
- pflag v1.0.9 *does* have typed errors — `NotExistError`, `ValueRequiredError`,
  `InvalidValueError`, `InvalidSyntaxError` in `errors.go` — added in
  [pflag v1.0.7](https://github.com/spf13/pflag/releases) (2025-07-16, PR #425). They are
  reachable by `errors.As`. `ExitCodeFor` simply predates them and never used them.

Three further places where cobra's ordering fights the contract, all confirmed against
`command.go` in v1.10.2 and all already documented in `root.go`'s comments:

1. `ParseFlags` runs at `:919`, `ValidateArgs` at `:968`. Flags are parsed **before**
   positionals are validated, so a bad flag preempts an arg-level refusal. This is the
   documented hole where the compat exit-3 contract leaks (see the `uncoveredOrNoArgs`
   comment).
2. `--help` short-circuits at `:934-935` (`return flag.ErrHelp`) and `--version` at
   `:939-953`, both **before** `PersistentPreRunE` at `:980`. Any decision made in that
   hook is invisible to `drops --help` and `drops --version`. This is why the face is
   resolved at construction time and why `SetHelpFunc` had to be used as a seam.
3. `PersistentPostRunE` (`:1027`) is unreachable once `RunE` returns an error (`:1019`
   returns early) — precisely when a store handle would leak. `NewRootCmd` returns an
   explicit `cleanup()` closure instead of using the hook, and says so.

Also note: `SilenceUsage` and `SilenceErrors` are **both true** on root. Cobra's own error
reporting is switched off entirely; `Execute()` prints `drops: <err>` itself. In other
words, the project already uses ~none of cobra's error presentation.

### The fix, proven

Two seams close the whole hole. Built and measured (`scratchpad/sz/a_fix/`):

```go
// one call, at root; FlagErrorFunc() walks up to the parent (command.go:547),
// so every subcommand inherits it
root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
    return fmt.Errorf("%w: %v", service.ErrInvalid, err)
})

// one helper, wrapped around every Args: validator
func invalidArgs(p cobra.PositionalArgs) cobra.PositionalArgs {
    return func(c *cobra.Command, args []string) error {
        if err := p(c, args); err != nil {
            return fmt.Errorf("%w: %v", service.ErrInvalid, err)
        }
        return nil
    }
}
```

Before / after, same binary shape:

| argv | baseline | with both seams |
|---|---:|---:|
| `dep add x` (wrong argc) | 1 | **2** |
| `--nope` (unknown flag) | 1 | **2** |
| `--db` (missing value) | 1 | **2** |
| `--limit xx` (bad value) | 1 | **2** |
| `nosuchverb` | 1 | **2** |
| `add -q` (unknown shorthand) | 1 | **2** |
| `dep add x y` (valid) | 0 | **0** |

Cost: one `SetFlagErrorFunc` call, an 8-line helper, and wrapping 57 `Args:` sites.
The `errors.As` route over pflag's typed errors also works and is the fallback if a
narrower mapping is wanted per error kind, but `SetFlagErrorFunc` alone is sufficient
and is a single point.

**This is worth stating plainly: the exit-code defect is not an argument for leaving
cobra. It is an argument for using two seams cobra already provides. Every alternative
router would need the equivalent work, and two of them make it harder (§4).**

---

## 3. Which cobra features are actually used?

### Shell completion — **not used at all**

- Zero references to completion in production code. Zero `ValidArgsFunction`, zero
  `RegisterFlagCompletionFunc`, zero `ValidArgs:`. The only hits anywhere in the repo are
  a line of `ROADMAP.md` prose and `internal/planinv/verbs_test.go` asserting the verb
  *listing* — and `planinv` is on the cut list.
- No completion script is installed anywhere on this machine (`~/.zsh/completions`,
  `/usr/share/zsh/site-functions`, `~/.config/fish/completions` — nothing).
- The `justfile` install recipe symlinks the binary and does not generate completions.
- What is generated is 212 lines of zsh / 426 of bash that knows verb names and flag
  names and nothing else: no issue ids, no project slugs, no statuses, no labels. For a
  CLI whose primary user is a subprocess, this is not a feature — it is a verb in the
  public surface with no consumer.
- `completions.go` is **1020 of cobra's 6121 top-level lines** — a sixth of the library
  is dead weight here.
- `CompletionOptions{DisableDefaultCmd: true}` removes the verb from the surface. It
  saves **72 bytes** (3,809,420 → 3,809,348), so it is a surface cleanup, not a size win.

### Help generation — used, and it is the main thing cobra is buying

`root.RunE` is `cmd.Help()`; 324 lines of `Short`/`Long`/`Use` prose feed it; `drops
--help`, `drops <group> --help` and `drops help` all work with no code. `SetVersionTemplate`
is used to make `--version` agree with the `version` verb. `SetHelpFunc` is used, but only
for the `bd` face (cut).

### Persistent flag inheritance — used, but **not** the way the ticket assumed

There is exactly **one** `root.PersistentFlags()` block, registering ten flags: `--db`,
`--json`, `-P/--project`, `--all-projects`, `-a/--all`, `--inbox`, `-q/--quiet`,
`--no-color`, plus the two hidden compat flags.

Every one is bound **by pointer into the `App` struct** (`f.StringVarP(&app.projectFlag,
"project", "P", …)`). Subcommands read `app.projectFlag`, never `cmd.Flags().GetString("project")`.
There are **zero** calls to `InheritedFlags()`, `Parent()` or `Root()` in production code.
So the "inheritance" is Go variable binding, which any router gives you for free.

What cobra *does* give — and this **is** load-bearing — is that pflag accepts those flags
in any position at any depth. Verified against the real binary:

```
drops --project inbox list      exit 0    drops list --project inbox   exit 0
drops list -P inbox             exit 0    drops -P inbox --json list   exit 0
drops --inbox --json ready      exit 0    drops list -aq               exit 0   (bundling)
drops q -- "--not-a-flag ..."   exit 0    (terminator)
```

That is GNU-style parsing — flags interspersed with positionals, shorthand bundling,
`--` terminator — not the flag *tree*. It is the single most important behavioural
requirement, and it is what disqualifies half the alternatives (§4).

### Also used

- `Args:` validators — 57 sites, five kinds
- `Annotations` — two keys (`nostore`, `nosync`), 18 sites, driving `PersistentPreRunE`'s
  "skip the store" and "skip auto-sync" branches
- `PersistentPreRunE` — one, doing lazy store open, cwd default, and the amortized mirror
  sync. Genuinely useful: `version` and `config` never touch the database.
- `Hidden` / `MarkHidden` — for `dolt` and the compat flags, both cut
- `SilenceUsage` / `SilenceErrors` — both true, i.e. cobra's error output is disabled

### Not used

Command groups (`GroupID`), `SuggestFor`, `ValidArgs`, `MarkFlagsMutuallyExclusive`,
`MarkFlagRequired`, `MarkFlagsOneRequired`, `EnableTraverseRunHooks`, `SetUsageTemplate`,
`SetHelpTemplate`, `DisableFlagParsing`, doc generation (`cobra/doc`), and all of
completion.

---

## 4. The alternatives

All five built as the **same** minimal tree (root with the eight real scoping flags, a
nested `dep add <id> <blocker> --type`, and a `list --limit`), so the numbers are
comparable. Sources in `scratchpad/sz/`.

| router | version | binary | Δ over empty | wiring lines, same tree | interspersed flags | exit-code control |
|---|---|---:|---:|---:|---|---|
| **cobra + pflag** | v1.10.2 / v1.0.9 | 3,809,420 | +1.46 MB | 54 | **yes** | caller's, two seams |
| stdlib `flag` (hand-rolled dispatch) | — | 2,502,830 | +0.15 MB | 126 | **no** | fully caller's |
| `peterbourgon/ff` | v4.0.0-beta.1 | 3,203,332 | +0.85 MB | 52 | **no** | fully caller's |
| `urfave/cli` | v3.11.0 | 5,664,036 | +3.31 MB | 46 | yes | **library's** (`OsExiter`) |
| `alecthomas/kong` | v1.16.1 | 6,380,660 | +4.03 MB | 44 | yes | library default 80, overridable |

Empty Go binary baseline: 2,350,980 bytes.

### stdlib `flag` — disqualified on the CLI contract

Go's `flag` **stops parsing at the first non-flag argument**. Demonstrated:

```
$ demo x y --type related --json
type="blocks" json=false positionals=["x" "y" "--type" "related" "--json"]
$ demo --type related --json x y
type="related" json=true  positionals=["x" "y"]
```

`README.md` and `docs/agents/issue-tracker.md` document flags-after-positionals in
sixteen distinct places — `drops create "title" -t bug`, `drops close <id> --reason …`,
`drops show <id> --json`, `drops update <id> -d …`, `drops memory edit <id> --group …`,
`drops project add --slug …`. Every one of those breaks. `flag` also has no shorthand
bundling (`-aq`), so `-aq` becomes an unknown flag named `aq`.

To keep the contract you must write a GNU-style parser. That is what pflag is, and pflag
is **6249 lines** (1287 in `flag.go` alone). Saving 1.46 MB in a 15 MB binary by
reimplementing 6000 lines of well-tested parsing is not a trade this rewrite should make.

My own 126-line hand-rolled attempt already needed an ad-hoc `needsValue()` lookup table
to find the verb past a global flag's value — a heuristic that is wrong the moment a new
value-taking flag is added and nobody updates the table. That is precisely the class of
defect this repo's own notes call its worst: silent, and invisible to a test that was
written from the same wrong mental model.

### Hand-rolled dispatch — same disqualification

The dispatch loop itself is cheap (~30 lines). The parser is not. See above.

Wins: ~1.4 MB of a 15 MB binary, 0 ms of startup, and no dependency. Loses: 6000 lines of
parsing you now own, help generation you now write, and the flag-position contract unless
you write pflag again.

### `peterbourgon/ff` v4 — disqualified twice

`flag_set.go:parseArgs` returns on the first non-dash token:

```go
noDash    = !isEmpty && arg[0] != '-'
parseDone = isEmpty || noDash
if parseDone {
    return leftover, nil // leftover should include arg
}
```

There is no option to intersperse. Confirmed by running it: `a_ff.bin dep add x y --type
related --json` reports "accepts 2 arg(s), received 5".

And it is not released. The Go module proxy lists only `v4.0.0-alpha.1` through
`v4.0.0-alpha.4` and `v4.0.0-beta.1` (2025-08-02). The GitHub releases API shows the
latest tagged release of `peterbourgon/ff` is **v3.4.0, 2023-07-20** — over two years
without a stable v4. Not a foundation for a binary that is about to take over the real
store.

### `urfave/cli` v3 — parses correctly, but takes the exit code away from you

v3.11.0 (2026-08-16), actively maintained, and it does parse interspersed flags
correctly.

The blocker is that it decides the process exit code itself. `OsExiter` is a
**package-level variable** defaulting to `os.Exit` (`errors.go:11`), called from
`errors.go:160,166` and `help.go:149,288,340`. Measured: `a_urfave.bin nope` exits **3**,
even though `main` ends in `os.Exit(1)` — the library exited first. For a CLI whose
entire contract is ten stable codes agents branch on, having the router call `os.Exit`
out of a global is the wrong shape. It is overridable, but the override is a mutable
package global, not a per-invocation option, and every path that reaches it has to be
found by reading the library.

It also costs **+3.31 MB** over baseline, 2.3× cobra's, for a smaller feature set.

### `alecthomas/kong` — the only serious contender, and it still loses

v1.16.1 (2026-08-09), actively maintained. Struct-tag declarative, which is genuinely
nicer to read: 44 wiring lines against cobra's 54 for the same tree, and the global
struct embeds cleanly so `-P/--project` reaches every leaf. Parsing is correct —
interspersed flags, bundling, `--`. Exit codes are controllable (`exit.go:10` sets
`exitUsageError = 80`, `kong.go:463` uses `k.Exit(1)`; both go through the injectable
`kong.Exit(...)` option).

What it costs:

- **+4.03 MB** over baseline, 2.8× cobra's — the largest of the five, because it is
  reflection-driven.
- **All 61 command declarations rewritten** from function-literal to struct-tag form.
  ~600 lines. That is not a port, it is a re-expression: `RunE` closures that capture
  `app` become methods on command structs with `Run(g *Globals)` signatures.
- **Every `PersistentPreRunE` concern re-expressed.** The lazy store open, the cwd
  default, the `nostore`/`nosync` annotation branches, and the amortized mirror sync all
  live in one hook today. In kong they become `BeforeApply`/`AfterApply` hooks and bound
  values, spread across the type graph. The single most subtle piece of the CLI layer
  gets less centralized, not more.
- Struct tags are strings the compiler does not check. A typo in `name:"all-projects"`
  is a runtime surprise; a typo in `f.BoolVar(&app.allProjects, "all-projects", …)`
  is at least a live reference to the variable.

Kong wins ~80 lines of wiring across the whole package (585 → ~500) and loses 2.5 MB of
binary, one centralized pre-run hook, and a week. For a rewrite whose stated goal is
"fewer subsystems and better module boundaries", trading a centralized hook for a
distributed one is the wrong direction.

---

## 5. Binary size and startup time

The claim under test: agents shell out to `drops` constantly, so process startup is on
the hot path. It is — but cobra is not the part of it that matters.

### Binary size

| binary | bytes |
|---|---:|
| empty Go program | 2,350,980 |
| + cobra (2-command tree) | 3,809,420 |
| modernc.org/sqlite alone | 9,508,162 |
| sqlite + hand-rolled stdlib dispatch | 9,600,810 |
| **sqlite + cobra** | **10,188,546** |
| real `drops` binary | 15,181,625 |

Cobra's marginal cost measured in isolation is 1.46 MB. Measured **next to modernc
sqlite — which is what `drops` actually is** — it is `10,188,546 − 9,600,810 =` **587,736
bytes, 574 KB**, because sqlite already links most of cobra's transitive dependencies
(`regexp`, `text/template`, `reflect`, `strconv`). That is **3.9% of the 15 MB binary**.
Removing cobra entirely, and hand-writing a GNU parser to replace it, would shrink `drops`
by under 4%.

### Startup time

Bench: 20 warm-up runs, then 200-400 timed runs, wall time / N, on an AMD Ryzen 7 9700X.

| what | ms/invocation |
|---|---:|
| `/usr/bin/true` (fork+exec floor) | 0.243 |
| empty Go binary | 0.51 – 0.55 |
| sqlite-only binary | 0.887 – 0.912 |
| sqlite + hand-rolled stdlib dispatch | 0.897 – 0.911 |
| **sqlite + cobra** | **0.850 – 0.895** |
| `drops version` | 1.047 |
| `drops --help` | 1.104 |
| `drops list --json` (opens the store) | 2.00 |

**The sqlite+cobra and sqlite+stdlib figures are indistinguishable across three
independent runs — cobra measured marginally *faster* in two of them.** The difference is
inside the ±0.05 ms noise floor. Whatever the startup budget is spent on, it is not the
router.

Isolated in-process, against the real 61-command tree (Go benchmark, temporary
`_test.go`, since removed):

```
BenchmarkNewRootCmd-16       22399 ns/op   133458 B/op   681 allocs/op
BenchmarkExecuteVersion-16   33501 ns/op   152748 B/op   876 allocs/op
```

Building the entire command tree costs **22 µs**; building it *and* dispatching `version`
end to end costs **33.5 µs**. Against `drops version`'s 1047 µs of wall time, cobra is
**3.2%** — and against `drops list --json`'s 2000 µs, **1.7%**.

Package init, from `GODEBUG=inittrace=1` on the real binary:

```
init github.com/spf13/pflag  0.001 ms
init github.com/spf13/cobra  0.001 ms
                     ------------------
total across 68 packages     0.287 ms
```

Cobra and pflag together are **0.002 ms of 0.287 ms — 0.7% of package init, 0.2% of the
process.** The actual init cost is `modernc.org/libc` (0.049), `go-humanize` (0.033),
`internal/service` (0.029) and `modernc.org/sqlite/lib` (0.022).

**Conclusion on the hot path.** A `drops` invocation costs about 1 ms cold and 2 ms when
it opens the store. Of that, 0.24 ms is the kernel's fork+exec floor that no program
escapes, ~0.3 ms is Go runtime and package init dominated by sqlite, and ~0.03 ms is
cobra. If startup ever needs to come down, the levers are the sqlite driver and the 15 MB
of pages the loader touches — not the router. Removing cobra would recover about 30 µs
per invocation. At a thousand invocations a session that is **30 milliseconds**.

---

## 6. Do the tests lock cobra in?

No — which is what makes this a choice rather than an inheritance.

`internal/cli` has 12018 test lines. Coupling to cobra:

- **17** lines reference `cobra.` at all; three test files import it
- **26** calls to `NewRootCmd(`, **12** to `SetArgs(`, **21** to `.Execute()`
- **230** calls go through `RunForTest(args, dbPath, cwd) (stdout, stderr, error)` or the
  package's harness helpers — a router-agnostic seam: argv in, streams and error out

The three most cobra-coupled test files after `root_test.go` are `face_external_test.go`
(12), `compat_remap_test.go` (12) and `uncovered_test.go` (4) — all compat, all cut.

`RunForTest` is the right seam and it already exists. **Keep it in the rewrite regardless
of the router chosen**, because it is what makes this question answerable at all.

---

## 7. Recommendation

**The router is cobra (`spf13/cobra` v1.10.2, `spf13/pflag` v1.0.9+).** No migration.

Why each alternative loses:

- **stdlib `flag`** — cannot parse `drops close <id> --reason "…"`. Go's `flag` stops at
  the first positional. Sixteen documented invocations break. Fixing it means writing
  pflag's 6249 lines again.
- **hand-rolled dispatch** — same parser problem. Wins 574 KB of a 15 MB binary and 30 µs
  per invocation, in exchange for owning a GNU-style argument parser and a help generator.
- **`peterbourgon/ff` v4** — same non-interspersing parse (`flag_set.go:parseArgs`
  returns on the first non-dash token, with no option to change it), and no stable release
  in two years (latest stable is v3.4.0, 2023-07-20).
- **`urfave/cli` v3** — parses correctly, but calls `os.Exit` from inside the library via
  a package-level `OsExiter`, on five paths. Measured overriding the exit code of a
  program whose whole contract is ten stable exit codes. Also +3.31 MB.
- **`alecthomas/kong`** — the real contender. Loses on cost/benefit: ~80 fewer wiring
  lines against +2.5 MB of binary, a rewrite of all 61 command declarations, and the
  centralized `PersistentPreRunE` hook (lazy store open, `nostore`/`nosync` annotations,
  amortized sync) becoming distributed `BeforeApply` hooks across the type graph. Wrong
  direction for a rewrite whose goal is better module boundaries.

Cobra costs, stated honestly: 585 lines of wiring after the cuts (~11 per command),
574 KB of binary next to sqlite (3.9%), ~30 µs per invocation (3%), 105 comment lines
warning about its execution order, and a library where a sixth of the code
(`completions.go`) is dead weight. Against that it supplies GNU-style parsing that the
documented CLI contract requires, help generation for 31 verbs, a centralized pre-run
hook that keeps `version` and `config` from touching the database, and eleven years of
other people finding its edges.

### Carry these three changes into the rewrite

1. **Close the exit-code hole.** Add `root.SetFlagErrorFunc` wrapping pflag's error in
   `service.ErrInvalid`, and an `invalidArgs()` wrapper around every `Args:` validator.
   Proven above: every usage error moves from 1 to 2, valid invocations stay 0. This is
   a real defect in the shipped contract today, not a cosmetic change — an agent
   currently cannot distinguish "you called it wrong" from "it crashed".
   Then extend the README table: **2 covers unknown verbs, unknown flags, missing flag
   values, bad flag values, and wrong argument counts.**

2. **Decide about `completion` deliberately.** It is generated, in the public verb
   listing, referenced by a test, installed nowhere, and completes nothing a drops user
   would want (no ids, no project slugs, no statuses). Either
   `CompletionOptions{DisableDefaultCmd: true}` and drop it from the surface, or register
   `ValidArgsFunction` for issue ids and project slugs and make it worth having. Shipping
   a verb with no consumer is the thing this rewrite is supposed to stop doing.

3. **Keep `RunForTest` as the test seam.** 230 of the package's test call sites already
   go through it and only 76 references touch cobra directly. That property is why this
   question was answerable by measurement, and it is worth preserving deliberately rather
   than by accident.

---

## Sources

**Direct measurement** (`/home/ted/src/drops.orig` at 2026-09-01, Go 1.27.0 toolchain,
AMD Ryzen 7 9700X, Linux 7.2.2):
- `go/ast` line classifier over `internal/cli/*.go`
- `go test -bench` on `NewRootCmd` / `Execute` (temporary `_test.go`, removed)
- `GODEBUG=inittrace=1` on the shipped binary
- process-startup harness, 20 warm-up + 200-400 timed runs
- five comparison CLIs built to an identical command tree

**Library source** (module cache, read directly):
- `github.com/spf13/cobra@v1.10.2` — `command.go:905-1044` (`execute()` ordering),
  `command.go:547` (`FlagErrorFunc` parent walk), `args.go`, `completions.go:107-112,746`
- `github.com/spf13/pflag@v1.0.9` — `errors.go` (typed errors), `flag.go`
- `github.com/peterbourgon/ff/v4@v4.0.0-beta.1` — `flag_set.go:parseArgs`
- `github.com/urfave/cli/v3@v3.11.0` — `errors.go:11,160,166`, `help.go:149,288,340`
- `github.com/alecthomas/kong@v1.16.1` — `exit.go:10`, `kong.go:463`

**Version and release data** (Go module proxy `proxy.golang.org`, GitHub releases API):
- cobra v1.10.2, 2025-12-03 — latest
- pflag v1.0.7, 2025-07-16 — added typed errors (PR #425); v1.0.10 now current
- urfave/cli v3.11.0, 2026-08-16
- alecthomas/kong v1.16.1, 2026-08-09
- peterbourgon/ff v4.0.0-beta.1, 2025-08-02 — no stable v4; latest stable v3.4.0, 2023-07-20

**Reference implementation docs**: `README.md` §"Exit codes", `docs/agents/issue-tracker.md`,
`justfile`, `internal/cli/root.go` comments.
