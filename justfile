# justfile — drops. `just` 1.58.0+.
#
# Tests must never be able to reach a real store, and this is how that is
# enforced. `/dev/null` is not a directory, so `store.Open`'s MkdirAll fails with
# ENOTDIR and names the path: a test that calls `store.DefaultPath()` instead of
# taking an explicit path goes red, rather than quietly opening a store and
# passing. A merely-absent directory would NOT do this — Open creates it.
#
# It cannot silently disarm the way a repository path could: nothing gitignored
# or uncreated is involved, and drops is UNIX-only already (flock).
#
# Measured 2026-09-02: the suite passes with DROPS_DB unset — 416 tests, 1 skip —
# so nothing consults DefaultPath today. This is a tripwire for a test written
# later. It matters most AFTER cutover, when internal/store's guard is gone:
# `go test -tags dropscutover` with DROPS_DB unset was observed opening
# ~/.drops/drops.db itself, stopping only at the v6 schema check that the
# cutover removes.
#
# Exported by `test` alone. `build` and `install` produce a binary; what store
# that binary later opens is not the justfile's business.
no_default_store := "/dev/null/no-default-store-in-tests/drops.db"

# `mutate` takes arguments whose values contain spaces and braces — a mutation
# is a fragment of Go source. Interpolating {{ARGS}} into the recipe line hands
# them back to the shell to re-split, so `--old 'if measured.truncate {'`
# arrives as three arguments; "$@" under this setting preserves them exactly.
set positional-arguments

# `git describe` has no tag to work from until dw32p.28 cuts v1.0.0, so today it
# reports a bare short hash. The `-dirty` suffix is the load-bearing part: after
# cutover the binary on PATH is built from this working tree, and `drops version`
# is the only thing that can say it was built from a tree with uncommitted work.

# Build to ./drops, stamped with the commit it came from.
build:
    go build -ldflags "-X github.com/tedkulp/drops/internal/cli.Version=$(git describe --tags --always --dirty 2>/dev/null || echo devel)" -o drops .

# Run the full test suite the way CI does.
test:
    DROPS_DB="{{no_default_store}}" go test ./... -race

# A green `go test` is not a green build: vet reads the whole tree.
# `gofmt -l` exits zero while listing files, so test its output instead.

# go vet, plus a gofmt-clean check.
lint:
    go vet ./...
    @test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)

# The gate: lint, then the full race suite.
test-all: lint test
    @echo "all suites green"

# Prove the tests can fail. AGENTS.md holds every requirement clause to a
# control that has been mutated with its test observed red; this is those four
# steps as one command, over the controls catalogued in each package's
# mutations.json.
#
#   just mutate                  every catalogued control
#   just mutate render resolve   by package, or by control name
#   just mutate --list           what is catalogued, without running it
#   just mutate --restore        finish a run that was interrupted
#
# Deliberately NOT part of `test-all`. The gate answers "is the build green",
# which is a question about this commit; mutation answers "can these tests
# fail", which is a question about the suite, and pays a compile per control to
# do it. Run it when a ticket adds or changes a control.
#
# DROPS_DB is exported for the same reason `test` exports it: mutate spawns
# `go test`, which inherits this environment. tools/mutate carries the same
# fallback so a bare `go run ./tools/mutate` is equally safe.
#
# Built, not `go run`. Measured 2026-09-02: `go run` prints "exit status 3" to
# stderr and exits 1 itself, collapsing every exit code the harness makes to 1.
# That would lose the one distinction worth having here — a finding (1) against
# "a restore failed and the tree may still hold a mutation" (3) — which is the
# same defect class dw32p.1 found in cobra and fixed rather than inherited.

# Prove the tests can fail: mutate each catalogued control, require its test red.
mutate *ARGS:
    @go build -o .scratch/bin/mutate ./tools/mutate
    @DROPS_DB="{{no_default_store}}" .scratch/bin/mutate "$@"

# Refuses to install before cutover, and deletes itself when the guard does.
#
# Every ordinary build has `realStoreAllowed = false` and refuses
# ~/.drops/drops.db, so installing one would replace a working `drops` on PATH
# with a binary that cannot open the only store it exists to serve. dw32p.28
# removes internal/store/guard_cutover.go, and this check goes green on its own
# the moment it does. It runs before `test-all` so the refusal is immediate
# rather than arriving after the race suite.
_cutover-guard:
    @test ! -f internal/store/guard_cutover.go || { \
      echo "refusing: pre-cutover build. It refuses ~/.drops/drops.db (internal/store/guard.go)," >&2; \
      echo "so installing it would break drops on PATH. dw32p.28 removes the guard." >&2; \
      exit 1; }

# A copy, deliberately NOT the symlink the old repo used. That symlink pointed at
# a FROZEN repository; this one is under active development, so a symlink would
# make every `just build` — a half-finished one, a branch one — instantly the
# live `drops` for every agent session on this machine. The stale-binary failure
# the old repo's comment feared is answered by the version stamp instead: a copy
# that says `v1.0.0-14-gabc1234` tells you how stale it is, and staleness you can
# see costs minutes rather than an afternoon.
#
# It refuses a SYMLINK, inverting the old check. After cutover the path is a
# regular file this recipe owns; a symlink there means something else has taken
# over managing it, and this recipe is not entitled to guess.
#
# Gated on `test-all` because this binary is the live issue tracker: installing a
# red build breaks the tool you would use to record that it is broken.

# Copy this repo's build to ~/.local/bin/drops.
install: _cutover-guard test-all build
    @mkdir -p ~/.local/bin
    @if [ -L ~/.local/bin/drops ]; then \
      echo "refusing: ~/.local/bin/drops is a symlink -> $(readlink ~/.local/bin/drops)" >&2; \
      exit 1; \
    fi
    @install -m 755 drops ~/.local/bin/drops
    @echo "installed: $(~/.local/bin/drops version)"

# The one-time v6-to-v7 conversion, run once per machine. dw32p.28.
#
# Both machines clone this repository and convert their own ~/.drops/drops.db,
# so nothing here names one store's row counts: tools/cutover censuses whatever
# it is pointed at and hands that census to store.Rewrite as Expect, which rolls
# the transaction back rather than committing a conversion that disagrees.
#
#   just cutover --dry-run     rehearse on a throwaway copy, writing nothing
#   just cutover               convert for real, after backing up and proving it
#
# Built with -tags dropscutover because internal/store refuses ~/.drops/drops.db
# in every ordinary build. The tag becomes inert once dw32p.28 deletes the guard
# files, which is what lets the second machine run the same recipe from a clone
# that no longer has them.
#
# DROPS_DB is deliberately NOT exported here: this is the one command whose job
# is to open the real store.
cutover *ARGS:
    @go build -tags dropscutover -o .scratch/bin/cutover ./tools/cutover
    @.scratch/bin/cutover "$@"
