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
# This is the ONLY thing standing between a test and the real store.
# internal/store's guard was deleted at cutover (dw32p.28), because the binary
# this repository builds is the one that owns ~/.drops/drops.db. That guard was
# once a second line: a build without it, run with DROPS_DB unset, was observed
# opening the real store itself. Nothing refuses that path any more.
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
#
# Two things about `gofmt -l` shape this recipe. It exits ZERO while listing
# files, so its output is the signal and never its status. And it exits 2 on a
# path it cannot lstat, which is the failure `set -e` is here to catch: an
# empty list from a gofmt that never ran reads exactly like a clean tree.
#
# The file list comes from git rather than from walking `.`, which is d5xwd.
# `.` includes `/.scratch/`, the gitignored development-store directory, so a
# leftover `.go` script in there failed the gate for a reason that had nothing
# to do with the commit under test. `go vet ./...` never had the problem: it
# walks packages, and `.scratch/` is not one.
#
# `--cached --others --exclude-standard` is tracked files PLUS untracked ones
# git would offer to add, and it is the pairing that matters: the gate still
# sees a new .go file nobody has run `git add` on yet, so what it stops looking
# at is only what this checkout already treats as not its own. `--exclude-
# standard` is git's own exclude set, not `.gitignore` alone — it also reads
# `.git/info/exclude` and the user's `core.excludesFile`. That is deliberate.
# It makes the gate's idea of this tree the same one `git status` has, which is
# what makes a checkout under a globally-ignored path (`.worktrees/`, say — a
# second checkout of this repo at another commit) stay out of a formatting
# check that is asking about THIS commit. The price is that the untracked half
# is as machine-specific as `git status` is; the tracked half is not, because
# `--cached` ignores excludes entirely.
#
# An index entry deleted from the working tree is dropped by the `-f` test, so
# a half-done `rm` of a .go file is not reported as a formatting failure.
#
# The empty list is then asserted against, because it is how a broken listing
# would otherwise say "clean". `git ls-files` prints nothing and exits 128
# outside a work tree, and prints nothing at exit 0 for a copy of this tree
# living somewhere an enclosing repository ignores — a `git archive` export, a
# container COPY that dropped `.git`. Process substitution puts either status
# out of `set -e`'s reach. This is a Go module, so zero .go files is never a
# true answer, and saying so is a better guard than proving a work tree exists:
# it is THIS tree being listed that the check depends on.
#
# The git dependency is not new: `build` already shells out to `git describe`.
#
# `go vet ./...` has the same shape of blind spot in reverse, which is mkh3q:
# the right check over the wrong file set, in the other direction. `./...` is
# not "every package here" — the go tool drops `testdata` directories from it,
# along with any whose name starts with `.` or `_`. So a tracked `package main`
# under one is invisible to vet while the gofmt half above checks it, and
# `internal/lockfile/testdata/holder` is not a scrap of sample data: it is the
# real second process the lockfile contention tests build and run. A
# `fmt.Printf` type error planted in it left `just test-all` green.
#
# The uncovered set is therefore DERIVED, not named: the directories git counts
# minus the ones `go list ./...` reports. Spelling `testdata` into this recipe
# would have covered today's two directories and none of the other skip rules,
# and would go stale the day the go tool grows a third. Deriving it also means a
# helper added under a skipped directory later is vetted on arrival rather than
# when someone remembers this recipe exists.
#
# A directory under a nested `go.mod` is dropped from that list. `./...` could
# not name it either — `go vet` on a path in another module refuses with "main
# module does not contain package" — so vetting `tools/mutate/testdata/fixture`
# would mean building a second module, and its whole role is to be a small,
# deliberately-mutated corpus for the mutate tool's own tests. Without this
# guard a clean tree goes red, so it is load-bearing rather than defensive.

# go vet over the module plus the packages `./...` skips, and a gofmt-clean
# check over every file git counts as part of the tree.
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    files=()
    while IFS= read -r -d '' f; do
      if [ -f "$f" ]; then files+=("$f"); fi
    done < <(git ls-files -z --cached --others --exclude-standard -- '*.go')
    if (( ${#files[@]} == 0 )); then
      echo "lint: git listed no .go files here; this is a Go module, so that is a broken listing, not a clean tree" >&2
      exit 1
    fi
    mod=$(go list -m)
    declare -A covered=()
    while IFS= read -r ip; do
      rel=${ip#"$mod"}; rel=${rel#/}; [ -z "$rel" ] && rel=.
      covered[$rel]=1
    done < <(go list ./...)
    declare -A seen=()
    vetdirs=()
    for f in "${files[@]}"; do
      d=${f%/*}; [ "$d" = "$f" ] && d=.
      [[ -v seen[$d] ]] && continue
      seen[$d]=1
      [[ -v covered[$d] ]] && continue
      p=$d nested=
      while [ "$p" != "." ]; do
        if [ -f "$p/go.mod" ]; then nested=1; break; fi
        if [[ $p == */* ]]; then p=${p%/*}; else p=.; fi
      done
      [ -n "$nested" ] || vetdirs+=("./$d/")
    done
    go vet ./... ${vetdirs[@]+"${vetdirs[@]}"}
    unformatted=$(gofmt -l "${files[@]}")
    if [ -n "$unformatted" ]; then printf '%s\n' "$unformatted"; exit 1; fi

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
# It refuses a SYMLINK, inverting the old check. The path is a regular file this
# recipe owns; a symlink there means something else has taken over managing it,
# and this recipe is not entitled to guess.
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
