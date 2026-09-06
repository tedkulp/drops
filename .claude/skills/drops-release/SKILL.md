---
name: drops-release
description: Use when cutting a release of drops — running the gate, moving the changelog's Unreleased entries into a dated section, tagging, and pushing so GoReleaser publishes. Covers what the tag triggers and how to verify it landed.
---

# Releasing drops

A release is a pushed `v*` tag. `.github/workflows/release.yml` runs GoReleaser,
which builds linux and darwin binaries for amd64 and arm64, publishes a GitHub
Release with archives, checksums, `.deb` and `.rpm`, and updates the Homebrew
cask in `tedkulp/homebrew-tap`. Everything after the push is automatic; this
skill is the part that is not.

Work through the steps in order. Each one is a thing that has to be true before
the tag exists, because **a pushed tag cannot be taken back** — a re-tag
republishes over a release people may already have fetched.

## Step 1 — Run the gate

```sh
just test-all
```

`go vet` over the packages `./...` skips, a gofmt check over what git counts as
part of the tree, then `go test ./... -race` with `DROPS_DB` pointed at the
tripwire. Nothing else counts as green: a bare `go test` is a weaker check, and
`gofmt -l` exits 0 while listing files, so read the recipe's verdict rather than
an exit code you inferred.

## Step 2 — Mutate, if this release changed a control

`just mutate` is deliberately not in the gate or in CI. It asks whether the
suite can fail, not whether this commit is green, and pays a compile per
control.

Run it when the release contains a ticket that added or changed a control:

```sh
just mutate                     # everything, if you are unsure
just mutate <package>           # or scope it to what changed
```

Exit 0 is every control red. **Exit 1 is a finding, never a pass** — a test that
cannot fail for the clause it appears to cover. Fix the test before releasing.
Exit 3 means a restore failed and the tree may still hold a mutation; run `just
mutate --restore` and do not tag until it is clean.

## Step 3 — Decide the version

```sh
git tag --sort=-version:refname | head -5
```

Semver against the last tag:

- **Patch** (`0.1.0 → 0.1.1`) — fixes only.
- **Minor** (`0.1.x → 0.2.0`) — new verbs or flags, backwards-compatible.
- **Major** (`0.x.x → 1.0.0`) — a break in the CLI contract.

drops is pre-1.0, and `docs/agents/issue-tracker.md` is what agents drive it by,
so treat a changed verb shape as at least a minor.

## Step 4 — Move the changelog's Unreleased entries

`CHANGELOG.md` follows Keep a Changelog. Per AGENTS.md, **only release work
moves those entries**, which makes this the step that has been waiting for you.

Add a dated section directly below `## [Unreleased]`, and leave `##
[Unreleased]` in place and empty:

```markdown
## [Unreleased]

## 0.2.0 — 2026-09-14

### Added
- ...
```

**This repository's heading format is `## 0.2.0 — 2026-09-14`**: no brackets,
no leading `v`, and an em dash rather than a hyphen. It is not the bracketed
form other repositories use. Match `## 0.1.0 — 2026-09-03`, which is already in
the file.

Categories in use here: `Added`, `Changed`, `Removed`, `Fixed`, `Internal`.
Keep a Changelog's `Deprecated` and `Security` are available if a release needs
them.

Use today's real date. Check it rather than recalling it:

```sh
date -u +%Y-%m-%d
```

## Step 5 — Commit

```sh
git add CHANGELOG.md
git commit -m "Release v0.2.0: <one-line summary of what changed>"
```

Commit before tagging, so the tag contains the changelog it describes.

## Step 6 — Tag and push both

```sh
git tag v0.2.0
git push && git push origin v0.2.0
```

`git push` alone does **not** carry the tag. Two commands, and the second is the
one that starts the release.

## Step 7 — Verify

```sh
gh run list --limit 5
gh run watch
```

Then check what was actually published:

```sh
gh release view v0.2.0
```

Expect eight archives and packages plus `checksums.txt`: `tar.gz` for
linux/darwin × amd64/arm64, and `.deb` and `.rpm` for the two linux arches.

Last, confirm the binary knows what it is. Download one and run it:

```sh
drops version    # drops v0.2.0 (abc1234, built 2026-09-14T…Z)
```

A release that prints `drops devel`, or that says `modified`, is a stamping
failure — see *When it goes wrong* below.

## What the tag triggers

| Artifact | Where it lands |
|---|---|
| `drops_<version>_{linux,darwin}_{amd64,arm64}.tar.gz` | GitHub Release assets |
| `drops_<version>_linux_{amd64,arm64}.{deb,rpm}` | GitHub Release assets |
| `checksums.txt` | GitHub Release assets |
| Homebrew cask `drops` | `tedkulp/homebrew-tap` |

## The one secret this repository needs

`HOMEBREW_TOKEN`, in the drops repository's Actions secrets: a token with push
access to `tedkulp/homebrew-tap`. Everything else uses the `GITHUB_TOKEN`
GitHub mints for the run, so the archives, packages, checksums and the GitHub
Release itself cannot fail for a missing credential.

```sh
gh secret list --repo tedkulp/drops
```

**An empty list means the tap push will fail** — and it fails at the *last*
step, after the GitHub Release has already published, which is the confusing
shape of it: most of the release worked. The release is still good; only the
cask is stale. Add the secret and re-run the job rather than re-tagging.

```sh
gh secret set HOMEBREW_TOKEN --repo tedkulp/drops   # reads the token from stdin
gh run rerun <run-id> --failed
```

## When it goes wrong

- **`drops version` says `devel`.** The `-X` stamp named a symbol that does not
  exist. `go build` does not error on that; it drops the stamp. `release_test.go`
  guards it, so run `just test-all` and read what it says.
- **`drops version` says `modified`.** Go's `vcs.modified` comes from `git
  status --porcelain`, which counts untracked files. Something not in
  `.gitignore` was in the tree at build time.
- **The release aborted on a missing file.** `.goreleaser.yaml`'s `files:` names
  something that is gone. The tag is already pushed at this point, so this is
  worth catching earlier: `release_test.go` checks it, and CI runs `goreleaser
  check` on every push.
- **The tag was pushed with a broken config.** Delete both the local and remote
  tag, fix, and re-tag *only* if the release did not publish. If it did,
  release a new patch version instead.

## Checking the config without releasing

CI runs `goreleaser check` on every push and PR. To run it yourself:

```sh
goreleaser check
goreleaser release --snapshot --clean    # a full local build, publishing nothing
./dist/drops_linux_amd64_v1/drops version
```

A snapshot writes to `dist/`, which is gitignored precisely so it cannot make
the next build report `modified`.

## Quick reference

| Step | Command |
|---|---|
| Gate | `just test-all` |
| Mutation, if controls changed | `just mutate` |
| Last tags | `git tag --sort=-version:refname \| head -5` |
| Today, in UTC | `date -u +%Y-%m-%d` |
| Commit | `git commit -m "Release vX.Y.Z: <summary>"` |
| Tag | `git tag vX.Y.Z` |
| Push both | `git push && git push origin vX.Y.Z` |
| Watch | `gh run list --limit 5` |
| Inspect | `gh release view vX.Y.Z` |
