# drops

A cross-project issue tracker for AI coding agents. One SQLite database for
every repository on the machine, with the project resolved from the working
directory — an agent that `cd`s into a checkout is already scoped to it.

Go, pure-Go SQLite (`modernc.org/sqlite`), and Cobra. UNIX-only: it serializes
with `flock`.

## What it does

- **One store, every project.** `~/.drops/drops.db` holds the issues and
  memories for every repository. `drops ready` inside a checkout answers for
  that checkout; `-P <slug>` overrides, `--all-projects` spans everything.
- **Built to be driven.** Every verb takes `--json` and exits with a stable
  code: 0 fine, 1 failed, 2 called wrong. `drops q "title"` captures an issue
  and prints its id, nothing else.
- **Memories.** Durable notebook entries an agent writes and searches, each
  owned by one project. Nothing is ranked or injected into a session — a memory
  is read because something asked for it.
- **Two machines, no server.** `drops sync` exports the store as deterministic
  JSONL, commits it to a mirror repository, and imports the other machine's.
  Every replicated record carries a `(generation, replica_key)` revision and
  removals carry versioned tombstones, so merging needs no server, no CRDT and
  no conflict UI, and a losing revision stays recoverable from Git history.

## Install

A tagged release publishes a static binary for linux and darwin, on amd64 and
arm64. There is no Windows build and there will not be one: drops serializes
with `flock`.

**Homebrew**, on macOS. It is a cask, so it is macOS-only — Homebrew on Linux
does not install casks; use one of the rows below there:

```sh
brew install --cask tedkulp/tap/drops
```

**Debian or RPM**, from the [latest
release](https://github.com/tedkulp/drops/releases/latest):

```sh
sudo dpkg -i drops_<version>_linux_amd64.deb     # or
sudo rpm -i  drops_<version>_linux_amd64.rpm
```

**An archive**, for anything else. Each release also carries `checksums.txt`:

```sh
tar -xzf drops_<version>_linux_amd64.tar.gz
install -m 755 drops ~/.local/bin/drops
```

**From source**, which is how this repository's own checkout gets its binary.
Needs Go 1.27+ and [`just`](https://github.com/casey/just):

```sh
just install
```

That runs the gate, builds with a version and build-date stamp, and copies the
binary to `~/.local/bin/drops`. A copy rather than a symlink on purpose: this
repository is under active development, and a symlink would make every build the
live `drops` for every session on the machine. `drops version` reports the
commit and build date, so staleness is something you can see instead — and it
reports them the same way whichever of these you used.

## Use

```sh
cd ~/src/some-repo
drops q "the button double-fires"      # capture; prints k3f9x
drops ready                            # what is actionable here
drops show k3f9x --json                # read one, for a machine
drops close k3f9x --reason "fixed in the debounce"
drops tui                              # browse it, for a human at a terminal
```

**[docs/agents/issue-tracker.md](docs/agents/issue-tracker.md) is the
contract**: every verb, how a project is resolved from a directory, what ids
mean and why nothing parses one, and how a long body gets in. It is what agents
read to drive drops, and a verb that changes shape without that file changing is
treated as a defect.

## Syncing two machines

`drops sync` makes `~/.drops` a Git repository itself and commits one file to
it. Point that repository at a remote once per machine:

```sh
git -C ~/.drops remote add origin git@example.com:you/drops-mirror.git
drops sync
```

Every export names its predecessor heads, so a snapshot that does not descend
linearly from the head this store last accepted is refused as a fork before
anything is committed. `drops replica rekey` rotates a machine's future identity
without rewriting the history it already authored, and `drops doctor` checks the
store's integrity and its FTS indexes.

## Status

Pre-1.0: `drops tui` has landed and a good deal of cleanup comes first.
`drops version` says which build you have, and [CHANGELOG.md](CHANGELOG.md) says what is in it.

## Working on drops

Read **[AGENTS.md](AGENTS.md)** first: the gate, the test discipline every
ticket is held to, and the tools in this environment that report success while
failing. [CONTEXT.md](CONTEXT.md) is the glossary. `just --list` is the rest.

Issues for this repository live in drops itself, not in a GitHub or GitLab
issue tracker.

CI runs the same gate on every push and pull request — `.github/workflows/ci.yml`
calls `just test-all` rather than respelling it — and checks that
`.goreleaser.yaml` still loads. Pushing a `v*` tag publishes a release;
`.claude/skills/drops-release/SKILL.md` walks that end to end.
