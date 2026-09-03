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
# This is now the ONLY thing standing between a test and the real store.
# internal/store's guard was deleted at cutover (dw32p.28), because the binary
# this repository builds is the one that owns ~/.drops/drops.db. Before that,
# `go test -tags dropscutover` with DROPS_DB unset was observed opening the real
# store itself, stopping only at a v6 schema check that no longer exists.
#
# Exported by `test` alone. `build` and `install` produce a binary; what store
# that binary later opens is not the justfile's business.
no_default_store := "/dev/null/no-default-store-in-tests/drops.db"

# `mutate` takes arguments whose values contain spaces and braces — a mutation
# is a fragment of Go source. Interpolating {{ARGS}} into the recipe line hands
# them back to the shell to re-split, so `--old 'if measured.truncate {'`
# arrives as three arguments; "$@" under this setting preserves them exactly.
set positional-arguments

# dw32p.28 cut v0.1.0 — 1.0 waits on the TUI and a good deal of cleanup.
#
# Only two things are stamped. The commit and whether the tree was modified are
# NOT: Go embeds vcs.revision and vcs.modified in every binary built from a
# repository, and a value the toolchain cannot get wrong beats one this recipe
# has to remember to pass. That is also why `git describe` no longer asks for
# --dirty — dirtiness now has one source instead of two that could disagree.
#
# The build date has no such source: Go records the commit's time, never the
# build's, and build time is the one that answers "how stale is the binary on my
# PATH". It is the reason this repo installs a copy rather than a symlink.

# Build to ./drops, stamped with its version and build date.
build:
    go build -ldflags "\
      -X github.com/tedkulp/drops/internal/cli.Version=$(git describe --tags --always 2>/dev/null || echo devel) \
      -X github.com/tedkulp/drops/internal/cli.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o drops .

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

# A copy, deliberately NOT the symlink the old repo used. That symlink pointed at
# a FROZEN repository; this one is under active development, so a symlink would
# make every `just build` — a half-finished one, a branch one — instantly the
# live `drops` for every agent session on this machine. The stale-binary failure
# the old repo's comment feared is answered by the version stamp instead: a copy
# that says `v0.1.0-14-gabc1234` tells you how stale it is, and staleness you can
# see costs minutes rather than an afternoon.
#
# It refuses a SYMLINK, inverting the old check. After cutover the path is a
# regular file this recipe owns; a symlink there means something else has taken
# over managing it, and this recipe is not entitled to guess.
#
# Gated on `test-all` because this binary is the live issue tracker: installing a
# red build breaks the tool you would use to record that it is broken.

# Copy this repo's build to ~/.local/bin/drops.
install: test-all build
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
# The -tags dropscutover build tag is inert now: it selected the one build
# internal/store would let near ~/.drops/drops.db, and that guard was deleted at
# cutover. It stays because the tag names no file, so the recipe is the same
# command on a machine that has not converted yet.
#
# DROPS_DB is deliberately NOT exported here: this is the one command whose job
# is to open the real store.
cutover *ARGS:
    @go build -tags dropscutover -o .scratch/bin/cutover ./tools/cutover
    @.scratch/bin/cutover "$@"
