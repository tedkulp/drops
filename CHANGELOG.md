# Changelog

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

`drops` was rewritten from scratch for 0.1.0. The previous build is not an
ancestor of this one — no code was carried forward — so this file starts here
rather than continuing that history. What it replaced is described under
[0.1.0](#010--2026-09-03) as *the old build*, because most of what is worth
saying about this release is what changed relative to the thing people were
using.

## [Unreleased]

### Added

- **`drops tui`**, a full-screen two-pane issue navigator: the current
  project's live issues on the left, one issue in full on the right, and the
  right pane retargetable **without the list cursor moving**. The left pane is
  `list`'s row set — `open` and `in_progress`, in the same order every scanning
  verb uses — with the id and project columns capped at 12 columns, the type
  column dropped, and a compact ` [N]` on a blocked row. `/` filters on id and
  title, `C` includes closed, `a` spans every project, `enter` drops the two
  columns for the issue text, `w` soft-wraps, and `h l ← → 0 $` reach the bytes
  a page emits beyond the pane's width — 43% of open issues have some. The
  detail pane renders through `render.Console` unchanged, so a pane and a `show`
  cannot disagree about what an issue looks like. Unlike `list` and `ready`,
  `tui` **refuses to start and exits 2 where no project resolves**: a stderr
  advisory is invisible under an alt screen.
- **Following a relation from the navigator.** `f` opens a picker over the
  right pane's issue — grouped under a page's own headings, in a page's order —
  and `Enter` retargets the right pane onto the one you choose **without the
  list cursor moving**; `Backspace` walks back down the trail one level at a
  time, `Esc` drops all of it at once, and moving the list cursor clears it.
  The picker lists **all three dependency types**, where a page renders
  `blocks` alone, so a `discovered-from` edge is readable in drops for the
  first time. Its id column is auto-sized and uncapped, unlike the
  left pane's, because a picker's rows are siblings whose dotted ids share a
  prefix.
- **`Esc` is the navigator's universal go-back key.** It pops exactly one layer
  a press, in this order: an open picker, the `/` prompt, the help screen, the
  **whole** follow trail, the filter, and `enter`-zoom. With nothing live it
  does nothing, and at no depth does it quit — a mistyped `/` must not drop you
  out of a full-screen program. Dropping the whole trail is what `Esc` adds
  that nothing else could: `j` cleared the trail before this, but only by
  moving the list cursor, so there was no way back to the cursor's own issue
  that left the cursor on it — against the founding claim of the navigator.
  `Backspace` stays as the one-level motion. The help screen now **captures
  every key while it is up**, which is what makes that ordering total: nothing
  acts invisibly behind a screen that replaces the whole frame, and in
  particular `x` can no longer open a picker under the help that would then
  swallow the `Esc` meant to close it.
- **Writing from the navigator.** `x` opens one modal on whatever the right
  pane is showing — the followed issue when you are away from the cursor, the
  cursor's own otherwise — offering **close, reopen, comment, priority, claim
  and release**, each row present only where it applies. `priority` opens a
  second picker. `close` and `comment` open `$VISUAL`, then `$EDITOR`, then
  `vi`, on an empty `.md` temp file: an empty file or a non-zero editor exit
  writes nothing, so quitting the editor is the cancel. `reopen` is the one
  thing that asks first, because it clears `close_reason` — it names how many
  characters that is before discarding them. One message line above the footer
  carries a refused write and the credential scan, which under a full-screen
  program would otherwise go to a stderr nobody can see.
- **The navigator notices when somebody else writes.** Every two seconds it
  reads the store's `write_seq` — the counter bumped inside the same
  transaction as every replicated write, so another terminal, another agent and
  a `sync import` all move it — and puts `stale · R` on the footer when it has
  moved. It deliberately does **not** reload: this store's common writer is an
  agent, and a pane that reorders itself under a reader mid-thought is the
  thing the navigator exists to avoid. The check is one primary-key row, 4.2µs
  against the 1025-issue corpus, where a whole re-read is 5–10ms.
- **`r` hides the navigator's blocked rows**, leaving what you can actually
  pick up, and the footer says `ready` while it is on. It stacks with `/`, `a`
  and `C` and survives all three. It is deliberately **not** `drops ready`: that
  verb forces `status = open` and so cannot see an `in_progress` issue at all,
  which would hide the issue you are working on the moment you pressed the key.
  This is a predicate over rows already on screen — the same open-blocker count
  the ` [N]` marker prints — so it costs no store read and leaves the row set as
  `list`'s contract. Refresh moves to **`R`** to make room for it.
- **`R` re-reads the store**, unconditionally, marker or no marker. It is a
  refresh and not a navigation, so the follow stack survives it, the cursor
  rides its tracked id wherever the row moved, the filter stands, and **the
  detail pane holds your scroll position**: the page is re-rendered and
  compared byte for byte, so an identical page never moves and a changed one
  keeps your offsets. Whether your place is held is decided on **what is on
  screen** — the same issue, at the same pane width — and never on which motion
  asked for the draw, so a page you never opened starts at the top however you
  arrived on it, including the refresh that moves the cursor on because the
  issue you were reading was closed from another terminal. `enter`-zoom, the
  `Esc` out of it and a wider window still start at the top, because they reflow
  the page into different lines; `/`, `C`, `a` and a taller window now leave you
  where you were, where before every one of them threw you back to line 0.
- **`y` copies the navigator's right-pane issue id** to the system clipboard.
  The id is the thing you leave the TUI to use — in a `drops show`, a commit
  message, a `dep add` — and until now the only way out was to read it off the
  screen and retype it. It names the **right pane's** issue, the one `x` already
  writes to, so a follow trail does not make two keys mean two things. It goes
  out as OSC 52, which needs no `xclip` or `pbcopy` and works over ssh, but is
  unacknowledged and not universally supported: the footer therefore says `sent
  <id> to the clipboard` — what was sent, not what your clipboard now holds.
- **`render.Ellipsis` and `render.Cols`** are exported, so the navigator's row
  geometry — which deliberately truncates an id, the one thing `render`'s own
  rule forbids — is built on `render`'s truncation primitive rather than a
  second implementation of it.
- **`drops claim` and `drops release`** make wayfinder ownership explicit:
  claiming refuses to overwrite another session, while releasing writes a null
  assignee that returns the issue to the frontier.
- **The routing data has a read path.** `drops project list --json` now carries
  each project's `workspace_bindings` and `repository_locators` — the two
  tables rungs 3 and 4 of the resolution ladder consult, which `project add`
  wrote and only `Resolve` ever read. Neither key goes absent; a project with
  none answers `[]`. Only one of the two travels, and the rows say which: a
  repository locator is replicated and carries a `revision`, a workspace
  binding is machine-local and carries none.
- **`drops config show` says what it compared and which rung answered.**
  Alongside `store.path` and `project.slug` it now prints `project.rule` (the
  rung: `project-flag`, `env-project`, `workspace-binding`,
  `repository-locator`, `workspace-prefix`, `inbox-fallback`), `git.root`, and
  `git.origin` — the origin **normalized**, which is the form rung 4 matches
  and the one fact here a reader cannot get from `git` by hand. Each key is
  absent rather than empty when there is nothing to say. Diagnosing a directory
  that would not route previously meant opening the store in `sqlite3`. The
  `git.*` keys describe the directory rather than the scope, so `--inbox` and
  `-P` still report them; and where two projects claim one origin the evidence
  goes to stdout **before** the exit 5, since that collision is the case the
  verb exists to explain.

### Changed

- **`core.IssueView` carries its relations resolved**, not as bare ids. Every
  relation now arrives named — id, title, status and tombstone state, with the
  edge type beside a dependency's far end — read in the **same transaction** as
  the issue itself and in one query, which is what `ViewIssue`'s "so a page
  cannot combine two revisions of the graph" always claimed and did not do. The
  titles a page printed came from one `core.Issue` read per related id, issued
  by `cli` after that transaction had committed: 220 un-transacted reads across
  the 174 open issues of the staged corpus, worst `br-fmg6` at 22, and 1984
  across all 1025. `show` is byte-identical — same relations, same natural id
  order, fewer transactions — verified over 2052 corpus invocations.

- **One adapter from a `core.IssueView` to a `render.Page`**, in the new
  `internal/view` package, replacing the private one in `internal/cli`. `show`
  and the TUI's detail pane render the same issue, and `cli` imports `tui`
  rather than the reverse, so leaving it in `cli` meant two adapters for one
  rendering. It is not folded into `render`: `render` imports `internal/model`
  and nothing else, and this adapter's input is a `core` type. `show --json`
  now derives its relations from the page instead of converting the view a
  second time, so the machine and human surfaces cannot disagree about which
  edges block. `show` is byte-identical over 4108 corpus invocations — all 1025
  issues as a page and as `--json`, a 40-id page both ways, and an unknown id.

- **`core.OpenBlockers` is exported**, and `Ready` and `Blocked` now honour a
  caller-supplied status set instead of overwriting it with `open`. Both are for
  the TUI's left pane, which lists `in_progress` rows and so needs a blocked-by
  count for rows `Blocked` cannot see, in one call per refresh rather than one
  per row. `drops ready` and `drops blocked` pass no status set and are
  byte-identical, verified over the 1025-issue corpus.

### Fixed

- **`Ctrl+C` quits `drops tui` while the `/` filter prompt is open.** The
  prompt still captures printable command keys such as `q`, `a`, and `C`, but
  no longer swallows the non-printable interrupt key.

- **An overflowing TUI footer closes its dim style.** Footer text is now
  truncated by display width before styling, preserving the trailing SGR reset
  instead of leaking dim foreground colour past a narrow frame.

- **`drops ready` and `drops blocked` see an `in_progress` issue.** Both drew
  their candidates from `open` alone, so the issue you had actually started was
  in neither: `ready` could not offer you the work in front of you and `blocked`
  could not tell you what it was waiting on. Measured across the store, `ready`
  160 + `blocked` 13 came to 173 against a live set of 174. Their candidates are
  now `list`'s — `open` **and** `in_progress` — so the two partition one set
  between them and every live issue is in exactly one. Closed stays out of both,
  which is still why `-a` is refused rather than ignored, and a deferred issue
  still leaves both until its deferral passes. `drops tui` is unaffected: its
  `r` key was always a predicate over the loaded rows rather than a call to
  `ready`.
- **The tree builds on macOS again.** `isTerminal` issued the terminal-attribute
  ioctl by its Linux-only name, `unix.TCGETS`, so `internal/cli` did not compile
  for darwin at all. The request is now a build-tagged constant — `TCGETS` on
  Linux, `TIOCGETA` on darwin and the BSDs — and a portability test cross-vets
  the tree for darwin on every run, since no test on the build machine can go
  red for a constant that only another `GOOS` rejects.

### Internal

- **The gate's gofmt check reads the tree from git, not from `.`.** `gofmt -l .`
  walked `/.scratch/` — the gitignored development-store directory — so a
  leftover `.go` script left there by an earlier session failed `just test-all`
  for a reason that had nothing to do with the commit under test. The check now
  formats `git ls-files --cached --others --exclude-standard -- '*.go'`:
  tracked files plus untracked ones git would offer to add, so a new file nobody
  has run `git add` on yet is still checked, and the gate's idea of this tree is
  `git status`'s rather than a second exclusion list to keep in step. An empty
  listing is refused instead of read as clean, since that is what a copy of the
  tree with no usable `.git` produces. `go vet ./...` never had the problem — it
  walks packages, and `.scratch/` is not one.
- **The gate vets the packages `go vet ./...` skips.** `./...` is not every
  package in the tree — the go tool drops `testdata` directories from it, and
  any directory whose name begins with `.` or `_`. So
  `internal/lockfile/testdata/holder`, the real second process the lockfile
  contention tests build and run rather than sample data, was checked for
  formatting but never vetted: a `fmt.Printf` type error planted in it left
  `just test-all` green. Those packages are now vetted by path, and the set is
  derived as the directories git counts minus the ones `go list ./...` reports
  rather than by naming `testdata`, so a helper added under any skipped
  directory is vetted on arrival. Directories under a nested `go.mod` are
  excluded, since `go vet` refuses a path in another module and
  `tools/mutate/testdata/fixture` is a deliberately-mutated corpus for the
  mutate tool's own tests. This is the second half of the same defect as the
  entry above: the right check over the wrong file set.

## 0.1.0 — 2026-09-03

First release, and the one that took over `~/.drops/drops.db` and the `drops`
name on `PATH`.

Not 1.0: the TUI and a good deal of cleanup come first.

### Added

- **A two-machine Git transport.** `drops sync` exports the store as a
  deterministic JSONL snapshot, commits it to a mirror repository, and pulls and
  imports the other machine's. Every replicated record carries a
  `(generation, replica_key)` revision; a higher generation wins, equal
  generations from different replicas are concurrent and broken by the replica
  key, and removable records carry versioned tombstones so absence from a
  snapshot never means deletion. Every export names its predecessor heads, so a
  snapshot that does not descend linearly from the head this store last accepted
  is refused as a fork before anything is committed. There is no server, no
  CRDT, no merge UI, and no second conflict database: losing revisions stay
  recoverable from Git history.
- **`drops replica rekey`**, which rotates this installation's future identity
  without rewriting the history it already authored.
- **`drops doctor`** — SQLite `quick_check`, `foreign_key_check`, and all three
  external-content FTS indexes against their source tables. `--repair` rebuilds
  a failed FTS index and nothing else; `--scan` reads every stored issue and
  memory for credentials and names what it finds without ever redacting it.
- **`drops version`** reports the release, the commit it was built from, whether
  that tree had uncommitted work, and when the binary was built.
- **A v7 schema.** STRICT typed tables, inline generation/replica revisions,
  versioned tombstones, one explicit parent record per child, three
  external-content FTS indexes, and typed local replica state.
- **An identifier can name an issue or a memory, never both**, and the schema is
  what guarantees it: every minted id is reserved in `id_owners`, and both tables
  carry a foreign key back to that reservation.
- **`just cutover`**, the one-time v6-to-v7 conversion, run once per machine. It
  censuses the store independently of the conversion and asserts that census as
  the conversion commits, so a conversion that disagrees with the measurement
  rolls back rather than half-succeeds. `--dry-run` rehearses on a throwaway copy.

### Changed

- **Every id shape from the old build survives verbatim.** Nothing parses an id,
  renumbers one, or repairs a prefix that no longer matches its project.
- **Memories are project-owned notebook entries.** `remember`, `memories`,
  `memory show`, `memory edit`, `supersede` and `forget` are what is left; a
  memory now always has exactly one owning project.
- **`install` copies the binary instead of symlinking it.** The old symlink
  pointed at a frozen repository; this one is under development, and a symlink
  would make every build instantly the live `drops` for every session on the
  machine. Staleness is answered by the version stamp instead.
- **Every store method takes a `context.Context`.**
- **`--json` and the mirror use `encoding/json/v2`.**
- **A project is identified across machines by an immutable opaque key.** Slugs
  and repository locators are shared metadata; workspace bindings are local and
  never sync.

### Removed

- **`planinv`, the `bd` compatibility surface, `migrate`, and the `dolt`
  spike.** Machinery for cancelled plans, a completed one-time job, and spike
  residue.
- **The memory recall path.** `recall`, `prime`, salience ranking, pinning,
  recall telemetry, memory kinds and custom keys are gone. Measured across the
  real store: `recall_count` was 0 for all 149 memories, so `drops recall` had
  never returned a memory in the store's lifetime.
- **Six columns that were never written**, and dependency type `related`, which
  had one row. Deferral was being done with a label 21 times while its dedicated
  column had never been used.
- **Shell completion.** No completion function was ever registered, no script was
  installed anywhere, and a generated one cannot complete an issue id — the only
  thing worth completing.

### Fixed

Defects measured in the old build and corrected here rather than reproduced:

- **Every cobra and pflag rejection exited 1**, so a caller could not tell "you
  called it wrong" (2) from "it crashed" (1). Every parser and positional misuse
  now exits 2.
- **An unknown command inside a group exited 0.** `drops comment edit …`
  reported success having written nothing.
- **`show` never printed the comment thread.** The renderer had one and was
  tested for it; the CLI never filled it in, so the documented headline
  behaviour of `show` was false.
- **`search` ignored `-a`**, making its default wider than `list`'s.
- **`label list` reproduced one issue's labels store-wide.**
- **`move` named no boundary**: it read both endpoints before the write, so
  every line printed the same project on both sides.
- **`memories --deleted` hid a memory that had been superseded and then
  forgotten**, and `memory edit -P` leaked a raw SQLite foreign-key error.
- **`config show --json` printed `key=value`** instead of JSON.
- **A fenced code block was reflowed** by the renderer, which treated the fence
  as an ordinary line.
- **`supersede --global` and `-P` were accepted and thrown away.**

### Internal

- **The test suite is held to mutation, not coverage.** Each requirement clause
  has a control — the production branch that makes it true — and that control has
  been mutated with its test observed red. `just mutate` runs the catalogue: 300
  controls, all red. Coverage is explicitly not the standard, because two
  packages sat at 100% function coverage while mutation found real defects in
  both.
- **Discovered facts are constructor parameters; real subsystems stay real.**
  There is deliberately no `Store` interface: every test runs against real
  temporary SQLite, real Git, and real `flock`.
- **`docs/agents/issue-tracker.md` is the CLI contract**, and a verb that changes
  shape without that file changing is treated as a defect.
