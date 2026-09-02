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

Recipes export `DROPS_DB` to `.scratch/drops.db`. Until the cutover ticket, the old
`drops` on `PATH` has sole ownership of `~/.drops/drops.db` — the store this
repository's own issues live in. Runtime code refuses the default real-store path
during development; run a local binary only through a guarded recipe.

Three tools lie about success here:

- `gofmt -l` exits zero while listing files. The file list is the signal, never `$?`.
- `grep -c` exits one on zero matches and silently skips the rest of an `&&` chain.
- `go test` prints `ok` for a package whose tests all skipped. Read the skip count
  and the test events before treating that output as proof.

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

Commit first. A restore command cannot distinguish a mutation from uncommitted work.

1. Snapshot the file and record its checksum.
2. Apply the mutation.
3. Run the one test that names this requirement. It must fail. A green run means the
   test does not cover the clause — fix the test, not the record.
4. Restore from the snapshot and verify the checksum matches.

### What a ticket records

Enumerate, in `close --reason`: each control (file and behaviour), the test that went
red, and confirmation of the restore. A bare count is unauditable — it is the form
that let a ticket record "one control" without anyone reading it as "one".

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

## Lessons retained from the reference build

- Re-check the premise before the next round of rigour, not after it.
- Measure before changing a documented fact. A confidently wrong count is worse than
  a stale one.

## Planning

This repository has no `.internal/` tree. Durable research belongs in `docs/`; live
plans and decisions belong in the issue tracker. The local `.gitignore` deliberately
overrides the workstation's global `.internal/` exclusion so accidental files cannot
disappear from status.
