# Working in drops

A cross-project issue tracker for AI coding agents. Go, pure-Go SQLite, and Cobra;
one database for every repository, with the project resolved from the working
directory.

## Skills

- **Issue tracker:** [docs/agents/issue-tracker.md](docs/agents/issue-tracker.md).
  Issues live in drops itself. Its **Wayfinding operations** section defines how to
  work the map.
- `codebase-design` for package shape and seams, `tdd` for build tickets, `grilling`
  plus `domain-modeling` for decision tickets.

## The gate

`just test-all`: `go vet` and a gofmt check, then `go test ./... -race`. A green
`go test` is not a green build.

The gofmt check formats what **git** counts as part of this tree — tracked files
plus untracked ones git would offer to add — and not everything under `.`. So an
unformatted `.go` file left in the gitignored `/.scratch/` does not fail the gate,
while a new file you have not run `git add` on still does. "What git counts" means
git's whole exclude set, not `.gitignore` alone: `.git/info/exclude` and your
`core.excludesFile` are read too, so the gate's view of this tree is exactly `git
status`'s view, and the untracked half of that is as machine-specific as `git
status` is. Tracked files are not — `--cached` ignores excludes.

`test` and `mutate` export `DROPS_DB` to `/dev/null/no-default-store-in-tests/drops.db`
— not a scratch store but a tripwire, so a test that reaches for `store.DefaultPath()`
fails with ENOTDIR and names the path. Since the cutover (`dw32p.28`) this is the
**only** thing between a test and the real store: the binary this repository builds
owns `~/.drops/drops.db`, so `internal/store`'s development guard is gone and nothing
in runtime code refuses that path any more. Issues live in the store the suite must
never open.

Four tools lie about success here:

- `gofmt -l` exits zero while listing files. The file list is the signal, never `$?`.
- `grep -c` exits one on zero matches and silently skips the rest of an `&&` chain.
- `go test` prints `ok` for a package whose tests all skipped. Read the skip count
  and the test events before treating that output as proof.
- `go run` exits 1 whatever its program exited, printing `exit status 3` to stderr.
  A recipe that needs the program's own exit code must `go build` and run the binary.

## Test discipline

The dominant defect in this codebase is a test that appears to cover a requirement
but **cannot fail for it**. Coverage does not detect it: `render` and `resolve` sit
at 100% function coverage and mutation still found defects in both. Annotated
red-checks found zero holes across the reference build; unannotated mutation found
every one.

So the standard is not a number. It is that each requirement has a **control** — the
production branch that makes it true — and that control has been mutated with the
test observed **red**.

### Which controls get mutated

One per **requirement clause**: a behavioural claim the ticket's question names. Not
one per test function — that turns the count into the goal and selects for cheap
mutations.

Where a clause is too coarse to mutate in one place, split it by **load-bearing
branch**: parsers, orderings, comparisons, conflict resolution, guards, refusals.
Those are where a wrong implementation still passes. A struct that round-trips does
not need a control; a comparator does.

### How a mutation is run

`just mutate`. The four steps are the recipe's, not yours:

1. Snapshot the file and record its checksum.
2. Apply the mutation.
3. Run the one named test, expecting failure. A pass is the finding: the test does
   not cover the clause — fix the test, not the record.
4. Restore from the snapshot and verify the checksum matches.

The snapshot is harness-owned, so **you no longer have to commit first**. `git
checkout` was never the restore here — a restore command cannot distinguish a
mutation from uncommitted work — and the harness never runs one. A run killed
between steps 2 and 4 leaves a journal naming exactly what to put back; the next
run refuses to start, and `just mutate --restore` finishes it.

Controls live beside the code they mutate, in each package's `mutations.json`: a
name, the requirement clause it proves, the file, the `old` → `new` edit, and the
test. `just mutate` runs every one; `just mutate core` scopes to a package or a
single control name; `--list` prints what is catalogued without running it; and
`--file/--old/--new/--test/--clause` runs one ad hoc, for while you are still
finding out whether a test can fail at all.

It refuses more than it accepts, and every refusal is a finding, never a pass:

| It says | It means |
|---|---|
| `GREEN` | the test passed with the branch broken, so it does not cover the clause |
| `NOT RUN` | no test of that name ran — `go test -run` matching nothing prints `ok` and exits 0 |
| `BUILD FAILED` | the mutation does not compile, so it proves nothing about the test |
| `NO MATCH` | `old` is absent or appears twice; a mutation must name exactly one branch |
| `BASELINE RED` | the test was already failing, so its red says nothing |

Exit 0 means every control went red, 1 that there is a finding, 2 that it was
called wrong, and **3 that a restore failed and the tree may still hold a
mutation**.

`just mutate` is deliberately not part of `test-all`. The gate asks whether this
commit is green; mutation asks whether the suite can fail, and pays a compile per
control to answer. Run it when a ticket adds or changes a control.

### What a ticket records

Enumerate, in `close --reason`: each control (file and behaviour), the test that went
red, and confirmation of the restore. `just mutate <package>` prints exactly that, so
the enumeration is a by-product rather than prose written from memory, and `--json` is
the same record for a machine. A bare count is unauditable — it is the form that let a
ticket record "one control" without anyone reading it as "one".

A control that is not in a `mutations.json` was proved once and can never be proved
again. Catalogue it.

### Seams

**Discovered facts become constructor parameters; real subsystems stay real.**

A fact a process learns about its environment — the writer, terminal-ness, measured
width, the pager, the clock, the ID source — is a struct field passed in, never
discovered at the point of use. `render` accepts all four of its environment facts,
so its whole suite is byte-exact literals with no fake anywhere; the reference build
discovered them and needed two package-level variables to make the same behaviour
reachable.

The datastore is not such a fact. There is deliberately no `Store` interface: every
test runs against real temp SQLite, real git, and real `flock`. `synctest` cannot
fake SQLite's busy-handler sleep, so lock-contention tests are real-clock. Making
these fakeable would trade a real defect class for a faster suite.

### Serialized contracts

For at least one case per serialized contract, run the real writer and decode its
bytes. Deriving both sides from the same description tests the description.

### The CLI is a contract

`docs/agents/issue-tracker.md` is what agents read to drive drops. A verb that
changes shape without that file changing is a defect.

Every command in the tree carries at least one boundary test through
`RunForTest(args, db, cwd)`, asserting what the doc says a reader observes: a
byte-exact row for a scanning verb, the JSON shape for a reading verb, the store
effect for a mutating verb. A census test walks the command tree and fails on any
command absent from the tested-verb registry, so skipping one is an edit to a visible
list rather than an omission.

## Changelog

`CHANGELOG.md` follows Keep a Changelog. Every change made after the latest
release goes under `## [Unreleased]` (create it when absent), grouped under the
appropriate change category. Only release work moves those entries into a dated
version section.

## Lessons retained from the reference build

- Re-check the premise before the next round of rigour, not after it.
- Measure before changing a documented fact. A confidently wrong count is worse than
  a stale one.

## Planning

This repository has no `.internal/` tree. Durable research belongs in `docs/`; live
plans and decisions belong in the issue tracker. The local `.gitignore` deliberately
overrides the workstation's global `.internal/` exclusion so accidental files cannot
disappear from status.
