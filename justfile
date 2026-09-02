# justfile — drops. `just` 1.58.0+.
#
# Recipes export a repository-local scratch store. Until the cutover ticket,
# the old `drops` on PATH remains the only binary allowed to open the real store.

scratch_db := justfile_directory() + "/.scratch/drops.db"
export DROPS_DB := scratch_db

# Build to a stable path.
build:
    go build -o drops .

# Run the full test suite the way CI does.
test:
    go test ./... -race

# A green `go test` is not a green build: vet reads the whole tree.
# `gofmt -l` exits zero while listing files, so test its output instead.
lint:
    go vet ./...
    @test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)

# The gate. Lint first so cheap failures precede the race suite.
test-all: lint test
    @echo "all suites green"
