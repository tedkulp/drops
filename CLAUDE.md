# drops

A cross-project issue tracker for AI coding agents. Go, pure-Go SQLite, and
Cobra; one database for every repository, with the project resolved from the
working directory.

The rewrite is carried by the wayfinder map **Rewrite drops from scratch**
(`dw32p`). Its frontier is the work queue; checked-in notes are not a parallel
plan.

## Agent skills

- **Issue tracker:** [docs/agents/issue-tracker.md](docs/agents/issue-tracker.md).
  Issues live in drops itself. Its **Wayfinding operations** section defines how
  to work the map.
- Use `codebase-design` for package shape and seams, `tdd` for build tickets,
  and `grilling` plus `domain-modeling` for decision tickets.

## Working in this repository

- `just test-all` is the gate: `go vet` and a gofmt check, then
  `go test ./... -race`. A green `go test` is not a green build.
- Recipes export `DROPS_DB` to `.scratch/drops.db`. Until the explicit cutover
  ticket, the old `drops` on `PATH` has sole ownership of
  `~/.drops/drops.db`. New runtime code must refuse to use the default real-store
  path during development; do not run a local binary outside the guarded recipe.
- This repository has no `.internal/` planning tree. Durable research belongs
  in `docs/`; live plans and decisions belong in the issue tracker. The local
  `.gitignore` deliberately overrides the workstation's global `.internal/`
  exclusion so accidental files cannot disappear from status.
- `gofmt -l` exits zero while listing files. The file list is the signal, never
  `$?`.
- `grep -c` exits one on zero matches and can silently skip an `&&` chain.
- `go test` prints `ok` for a package whose tests all skipped. Check skips and
  explicit test events before treating that output as proof.

## Lessons retained from the reference build

- Re-check the premise before the next round of rigour, not after it.
- The dominant defect is a test that appears to cover a requirement but cannot
  fail for it. Mutate the control and confirm the test turns red.
- Commit before mutation. A restore command cannot distinguish a mutation from
  uncommitted work; use a harness-owned backup and verify restoration.
- For at least one case per serialized contract, run the real writer and decode
  its bytes rather than deriving both sides from the same description.
- Measure before changing a documented fact. A confidently wrong count is worse
  than a stale one.
