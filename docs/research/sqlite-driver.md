# Pure-Go SQLite and Go, as of 2026-09-01

Research for `dw32p.2`. Question: what changed in pure-Go SQLite and in Go itself since the
old build was pinned in mid-August 2026, and does any of it change how the new store should
be written?

The no-CGo constraint is settled and not examined here. This is about the *implementation*
of that constraint.

**Reference under test:** `/home/ted/src/drops.orig`, pinning `modernc.org/sqlite v1.56.0`
and `go 1.26.5`.

Everything below marked **measured** was run on this machine (linux/amd64, AMD Ryzen 7
9700X, Go 1.27.0) rather than taken from a changelog. Measurement scratch lives outside the
repo and is disposable.

## Recommendation, up front

**Stay on `modernc.org/sqlite`. Bump to `v1.57.0`. Target Go 1.27.**

```
go 1.27.0

require modernc.org/sqlite v1.57.0   // pins modernc.org/libc v1.74.4 — do not float it
```

The driver choice is confirmed, not merely inherited. What changes is not *which* driver but
five things about how the store uses it, listed in [What this changes](#what-this-changes-about-the-store).

The single most consequential finding is not about SQLite at all: **Go 1.27 is out and
`encoding/json/v2` is generally available in it**, which lands directly on the JSONL mirror
and every `--json` CLI path.

---

## 1. `modernc.org/sqlite`: v1.56.0 → v1.57.0

**Current version: `v1.57.0`, released 2026-08-19.** Exactly one release ahead of the pin.

Source: the module proxy, which is the authority on what exists.

```
$ curl -s https://proxy.golang.org/modernc.org/sqlite/@latest
{"Version":"v1.57.0","Time":"2026-08-19T11:04:27Z",
 "Origin":{"VCS":"git","URL":"https://gitlab.com/cznic/sqlite", ...}}
```

### Nothing in it touches WAL, busy timeouts, or concurrent writers

This is the direct answer to the question asked. The full commit range `v1.56.0..v1.57.0`
(via the GitLab compare API) is twelve commits, and the [CHANGELOG][cl] for v1.57.0 has four
entries:

1. **A new opt-in `_defensive` DSN parameter** — turns on `SQLITE_DBCONFIG_DEFENSIVE`.
2. **A DSN rejection** for the one combination defensive mode would swallow silently
   (`_defensive=1` with `_journal_mode=OFF`).
3. **Licensing/vendoring fixes** — ships the sqlite-vec MIT notice, renames `SQLITE-LICENSE`
   to `LICENSE-SQLITE` so `go mod vendor` actually propagates it.
4. **Per-`Driver` registration methods** (`RegisterFunction`, `RegisterModule`, …) and
   promotion of `freebsd/386`, `freebsd/arm`, `netbsd/amd64` to fully supported.

Entry 4 has a behaviour change, but only for programs that construct their own
`sqlite.Driver` and call `vtab.RegisterModule`. `drops` does neither — it uses
`sql.Open("sqlite", dsn)` and no virtual table modules of its own. Entry 3 is documentation
and packaging. So for this codebase the upgrade is **`_defensive` plus nothing**.

[cl]: https://gitlab.com/cznic/sqlite/-/raw/v1.57.0/CHANGELOG.md

### The engine is byte-identical — measured

The bump does not move SQLite or libc:

| | v1.56.0 | v1.57.0 |
|---|---|---|
| `sqlite_version()` | 3.53.3 | 3.53.3 |
| pinned `modernc.org/libc` | v1.74.4 | v1.74.4 |
| `go.mod` | — | **byte-identical to v1.56.0** |

The v1.57.0 changelog says of its platform work: "the transpiled sources under `lib/` are
byte-for-byte what v1.56.0 shipped". The identical `go.mod` confirms it from the other side.
This is as low-risk as a dependency bump gets.

> **Do not chase `modernc.org/libc` separately.** Latest is v1.75.6, but the driver's
> changelog is emphatic and repeats it every release: "downstream modules must pin the exact
> `modernc.org/libc` version this module's `go.mod` pins" ([GitLab #177][177]). Take v1.74.4
> from the driver and leave it alone.

[177]: https://gitlab.com/cznic/sqlite/-/issues/177

### WAL, busy timeout and concurrency still behave — measured

Rather than trust the "no changes" reading, the three properties the store leans on were
measured directly at v1.57.0.

**Busy timeout genuinely waits, and genuinely gives up.** One connection holds a write lock
for 1.2s while a second tries to write:

```
busy_timeout=5000ms: waited 1.21s  err=<nil>
busy_timeout=100ms:  waited 100ms  err=database is locked (5) (SQLITE_BUSY)
```

The 5s timeout queues and succeeds; the 100ms timeout fails fast with `SQLITE_BUSY`. This is
exactly the contract the store's 5-second timeout depends on.

**WAL, pragmas and 16-way concurrent writers.** The store's real DSN
(`journal_mode(WAL)` + `busy_timeout(5000)` + `foreign_keys(ON)` + `_txlock=immediate`)
against 16 goroutines each doing `BEGIN IMMEDIATE` / `UPDATE` / `COMMIT`:

```
journal_mode=wal   busy_timeout=5000   foreign_keys=1
ok  16 concurrent writers (_txlock)   16/16 committed, no SQLITE_BUSY
```

**The real suite passes.** The strongest available evidence: `drops.orig` copied, bumped to
v1.57.0, and run at the shape its README calls CI (`go test ./... -race -cpu=1,4`).

- `internal/store` — **ok** (64.6s)
- `internal/cli` — **ok** (51.4s), including
  `TestConcurrentUpdatesThroughCLIStayConsistent`, the 16-way concurrent `update` race that
  motivated `_txlock=immediate`
- `internal/service` — **ok** (61.0s)
- `internal/migrate` — **FAIL**, one test

The single failure is `TestRunDoltKeysHonoursContextDeadline`, and it is **pre-existing and
environmental**, not caused by the bump. Verified by running it against the untouched
v1.56.0 repo:

```
runDoltKeys err = ... exec: "dolt": executable file not found in $PATH,
want context.DeadlineExceeded
```

It needs a `dolt` binary that isn't installed. It also lives in `internal/migrate` and tests
the `dolt` spike — **both explicitly cut by the map**. Zero regressions from v1.57.0.

### `_defensive` is a free hardening win — measured

The new flag is worth taking, and it is worth checking, because the changelog warns that
under defensive mode "writes to a virtual table's shadow tables (fts5's `_data`, `_idx` and
so on) … fail". `drops` runs `doctor` operations *against* FTS5 tables, so this could
plausibly have broken the integrity check.

It does not. Measured, with and without the flag:

| operation | `_defensive=0` | `_defensive=1` |
|---|---|---|
| `journal_mode` | wal | **wal** |
| insert via trigger → FTS | ok | **ok** |
| `INSERT INTO t_fts(t_fts, rank) VALUES ('integrity-check', 1)` | ok | **ok** |
| `INSERT INTO t_fts(t_fts) VALUES ('rebuild')` | ok | **ok** |
| direct write to `t_fts_data` shadow table | ok | **blocked** |

Defensive mode blocks exactly the thing that should never happen (hand-corrupting an FTS5
shadow table) and nothing the store or `doctor` actually does. Given the store already goes
out of its way on security — `0700` directory, `0600` on the db and both sidecars, chmod
before first write — this is the same posture for one DSN parameter.

Two limits the changelog states plainly and this note repeats: it is hardening, not a
sandbox for hostile database files, and it is a property of the **connection**, not the
file — a second handle opened without it is unrestricted.

---

## 2. Is `ncruces/go-sqlite3` a credible alternative?

**Yes, genuinely credible — and it should still lose here.** The axes are concrete.

**Current version: `v0.35.4`, released 2026-09-01** (the day of this research).

### The ticket's premise is out of date: it is no longer wasm-interpreted

This matters, so it goes first. `ncruces/go-sqlite3` was historically "the wasm one" — SQLite
compiled to wasm, executed by the `wazero` runtime. **That is no longer how it works.**

Measured — `wazero` is absent from the dependency graph entirely. What is there:

```
github.com/ncruces/go-sqlite3-wasm/v5 v5.0.35304
```

and that module's own README says:

> This repo contains a Go translation of SQLite … Most of the code here is machine
> translated using [`wasm2go`](https://github.com/ncruces/wasm2go).

The generated source confirms it: `// Code generated by wasm2go. DO NOT EDIT.` — ordinary
Go, compiled by the Go compiler.

So both candidates are now **C → machine translation → Go**. modernc goes C→Go directly via
`ccgo`; ncruces goes C→wasm→Go via `wasm2go`. The old "interpreted wasm is slow" objection is
dead, and any evaluation resting on it is stale. They must be compared on measurements, not
on architecture.

### Where ncruces wins

- **Newer SQLite.** 3.53.4 vs modernc's 3.53.3 — measured via `sqlite_version()`. Modest, and
  modernc's 3.53.3 is *patched* for the 3.53.3 journal-rollback data-corruption bug (see
  below), so this is not a straightforward win.
- **Ahead on Go 1.27.** It ships `driver/driver_127.go` and `conn_127.go` behind
  `//go:build go1.27`, implementing `database/sql/driver.RowsColumnScanner` (the new
  scan-direct-into-destination interface), plus a `json_v2.go`. modernc has **no** `go1.27`
  build-tagged files at all — measured by grepping both module trees. This is a real
  responsiveness signal, though `RowsColumnScanner` is an allocation optimisation, not a
  capability drops needs.
- **Richer extension surface.** Bundled `vfs/adiantum` and `vfs/xts` (encrypted VFS),
  custom FTS5 tokenizers (new in v0.35.4), R*Tree/Geopoly, bloom filters. drops needs none
  of it today.
- **Genuine `context` cancellation.** ncruces can interrupt a running statement mid-query.

### Where ncruces loses

**Binary size — the decisive one for a project whose stated goal is "single static binary".**
Identical probe program, same flags, measured:

| driver | binary | vs modernc |
|---|---|---|
| `modernc.org/sqlite v1.57.0` | 9,982,322 B (9.51 MiB) | — |
| `ncruces/go-sqlite3 v0.35.4` | 18,947,216 B (18.06 MiB) | **+90%** |

Nearly double, for a CLI that agents invoke constantly.

**Speed.** Same benchmark body, same schema, same DSN pragmas; `-count=5`, medians:

| benchmark | modernc v1.57.0 | ncruces v0.35.4 | ncruces is |
|---|---|---|---|
| Insert 1000 rows (one tx, FTS trigger) | 24.40 ms | 30.17 ms | **24% slower** |
| FTS5 `MATCH` + `bm25` over 5000 rows | 4.65 ms | 7.35 ms | **58% slower** |
| Point read by primary key | 3.81 µs | 5.07 µs | **33% slower** |

modernc wins all three, and wins **worst on FTS5 search** — which is precisely `drops search`
and `drops recall`, the hot paths.

**FTS5 is opt-in and breaks the `database/sql` seam** — see §3.

**Migration cost for zero functional gain.** Different DSN dialect, different error types,
different registration model, and a rewrite of every `Open` path. The map's budget is better
spent elsewhere.

### The other candidates are not candidates

Checked against the module proxy:

| module | latest | verdict |
|---|---|---|
| `zombiezen.com/go/sqlite` | v1.4.2, **2025-05-23** | Stale >1yr. Wraps modernc anyway, so it inherits its engine while adding a non-`database/sql` API. |
| `github.com/glebarez/go-sqlite` | v1.23.0, 2026-08-06 | Maintained, but a thin wrapper over modernc — a layer, not an alternative. |
| `crawshaw.io/sqlite` | v0.3.2, **2020** | Abandoned, and CGo. |
| `github.com/tailscale/sqlite` | 2026-03-06 | CGo. Excluded by the settled constraint. |

The real field is two drivers wide.

---

## 3. FTS5 in each candidate

### What the old build actually needs

Read from `/home/ted/src/drops.orig/internal/store/fts.go` and
`internal/store/migrations/`. **The ticket says two FTS5 indexes; there are three.**
`internal/store/fts.go` is explicit:

```go
var ftsTables = []string{"memories_fts", "issues_fts", "comments_fts"}
```

Defined across `0003_memories_fts.sql` (memories, issues) and
`0005_drop_beads_text_fields.sql` (issues rebuilt, comments). Any rewrite plan sized for two
indexes is sized wrong.

The concrete requirements are:

1. **External-content FTS5** (`content='issues'`, `content_rowid='rowid'`) kept in sync by
   triggers — not contentless, not standalone.
2. **`bm25()` with per-column weights** — `bm25(memories_fts, 2.0, 1.0)`, title weighted 2×.
3. **The `('integrity-check', 1)` command form.** `fts.go` is emphatic that the two-argument
   form is mandatory, and that this was measured rather than assumed: the plain form "returns
   nil for BOTH desync modes … A doctor built on the plain form would report every store
   healthy forever."
4. **The `'rebuild'` command**, for `doctor --repair`.

### Both support all four — but not equally conveniently

Measured with a probe creating two external-content FTS5 indexes and exercising all four
operations:

| requirement | modernc v1.57.0 | ncruces v0.35.4 |
|---|---|---|
| external-content FTS5 + triggers | ok | ok |
| `MATCH` + weighted `bm25` | ok | ok |
| `('integrity-check', 1)` | ok | ok |
| `'rebuild'` | ok | ok |
| **available from plain `sql.Open`** | **yes** | **no** |

modernc compiles FTS5 into the engine. Measured via `PRAGMA compile_options`:

```
ENABLE_FTS5
```

It is simply there. `sql.Open("sqlite", dsn)` and FTS5 works.

**ncruces requires per-connection registration, and that is a real architectural cost.**
Since **v0.35.0 (2026-06-11) FTS5 is a separately compiled, opt-in extension** — an explicit
breaking change, made "to keep the code size small". Importing `ext/fts5` is not enough; the
first probe attempt failed exactly this way:

```
FAIL create 2x external-content fts5   sqlite3: SQL logic error: no such module: fts5
```

`fts5.Register` takes a `*sqlite3.Conn` and must run on **every** connection in the pool. The
supported route is the driver's own constructor:

```go
db, err := driver.Open(dsn, fts5.Register)   // NOT sql.Open("sqlite3", dsn)
```

With that, everything passes. But note what it costs: the store can no longer obtain its
`*sql.DB` through the standard `database/sql` entry point. It must depend on
`ncruces/go-sqlite3/driver` concretely, and every future connection-scoped extension threads
through the same hook. For a package whose doc comment is "Nothing above this package writes
SQL", giving up the driver-agnostic seam to regain a feature the incumbent has built in is a
bad trade.

### One caveat on the doctor check

The naive desync recipe (deleting rows behind the index's back) did **not** produce a
detectable desync in the probe, on either driver — `integrity-check` still returned healthy.
`drops.orig`'s own `fts_test.go` does drive real desync, and those tests pass at v1.57.0. The
lesson for the rewrite is the one `fts.go` already records: **a `doctor` FTS check must be
tested against a genuinely desynced index, or it cannot fail for the requirement it claims to
cover.** That is this repo's documented worst defect class, and the FTS check is where it has
bitten before. Carry the test, not just the check.

---

## 4. Go: the ticket asks about 1.26, but 1.27 shipped

**Go 1.27.1 is the current stable release.** Measured against the official download endpoint:

```
$ curl -s 'https://go.dev/dl/?mode=json&include=all'
go1.27.1  stable
go1.27.0  stable
go1.26.8  stable      <- old build pins 1.26.5, three patches behind
```

The local toolchain is already **go1.27.0**. So the ticket's framing ("anything in Go 1.26 the
old build could not use") understates it: an entire major release landed.

### What the old build does not use — measured

Grepped across `drops.orig`:

| feature | occurrences |
|---|---|
| `log/slog` | **0** |
| iterators (`iter.Seq`, range-over-func) | **0** |
| `testing/synctest` | **0** |
| `t.Context()` | **0** |
| `b.Loop()` | **0** |
| `t.Chdir` | 3 |
| **context-aware DB calls** (`ExecContext`/`QueryContext`/`QueryRowContext`) in `internal/store` | **7** |
| **non-context DB calls** (`s.db.Exec`/`Query`/`QueryRow`) in `internal/store` | **73** |

That last pair is the most important number in this document.

### The changes that matter for the store

**a) `context.Context` is missing from 91% of the store.** 73 of 80 database calls in
`internal/store` are the non-context variants. This is not a style point — it is why no query
can be cancelled or deadlined, and it interacts directly with the 5-second busy timeout: a
contended write blocks a goroutine for up to 5s with no way for the caller to give up. The
migration path already proves the seam works (`migrate()` correctly pins a `*sql.Conn` and
uses `ExecContext`/`QueryContext` throughout). **A from-scratch store should take
`context.Context` on every method from the first line.** Retrofitting 73 call sites later is
the expensive order.

**b) `errors.AsType` (Go 1.26) kills the string-matching error checks.** The old build detects
constraint violations by matching driver error *text*, in two places:

- `internal/store/deps.go:44` — `strings.Contains(err.Error(), "UNIQUE constraint failed")`
- `internal/service/issues.go:138` — the same string, duplicated

That is fragile against any driver or SQLite message change. modernc exposes a typed error;
measured:

```
raw error text: "constraint failed: UNIQUE constraint failed: t.v (2067)"
typed *sqlite.Error: code=2067
  SQLITE_CONSTRAINT_UNIQUE=2067 match=true
errors.AsType[*sqlite.Error] ok, code=2067
```

So the check becomes, with no string in sight:

```go
import (
    "modernc.org/sqlite"
    sqlite3 "modernc.org/sqlite/lib"
)

if e, ok := errors.AsType[*sqlite.Error](err); ok && e.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
    // ...
}
```

Go 1.26's `errors.AsType` is the generic, type-safe form of `errors.As` — "type-safe, faster,
and, in most cases, easier to use". Verified working against the real driver error.

**c) `encoding/json/v2` is GA in Go 1.27 — the biggest single item here.** Measured by
compiling and running against the local toolchain with **`GOEXPERIMENT` empty**, so it is
generally available, not experimental:

```
$ go doc encoding/json/v2
package json // import "encoding/json/v2"
```

`drops` is JSON-heavy in two places that both matter: the `--json` output every agent parses
(the CLI contract), and the three JSONL files the mirror transport is built on. v2 brings
correct and much faster marshalling, and `encoding/json/jsontext` gives streaming,
token-level control — a natural fit for JSONL, where the format is one JSON value per line
and the old `json.Encoder` newline behaviour has to be worked around. Both were confirmed
compiling and running:

```
{"id":"k3f9x","title":"hello"} <nil>
jsontext value: {"id":"k3f9x","title":"hello"}
```

Because the mirror is being reshaped into a two-machine transport anyway, this is the one
moment where adopting v2 costs nothing extra.

**d) `testing/synctest`** — Go 1.27 adds `synctest.Sleep`. The old build has zero usage, and
its suite spends real wall-clock time on concurrency tests (`internal/store` 64s,
`internal/cli` 51s). Worth evaluating for the time-dependent tests, but note the honest
limit: `synctest` fakes *goroutine* time, and it cannot fake SQLite's internal busy-handler
sleep, which is real time inside translated C. **It will not speed up the busy-timeout
tests.** Use it for scheduling and deferral logic (`deferred_until`, `started_at`), not for
lock contention.

**e) Smaller, still useful.** `slog.NewMultiHandler` (1.26) if the rewrite wants structured
logging — the old build uses `log` once and nothing else, so this is greenfield. `B.Loop` no
longer blocks inlining (1.26), so benchmarks measure what they claim to. `T.ArtifactDir`
(1.26) gives failing tests a real place to write diagnostic output. Green Tea GC is on by
default in 1.26 — a 10–40% reduction in GC overhead, free.

### Which version to declare

Declare **`go 1.27.0`**, not `1.27.1`. The installed toolchain is go1.27.0 and `GOTOOLCHAIN`
is `auto`; declaring `1.27.1` forces a toolchain download on every machine that has 1.27.0.
Both drivers are satisfied — modernc requires `go 1.25.0`, ncruces `go 1.26.0`.

---

## 5. The `file:` URI handling — the README is stale

**The behaviour the question asks about no longer exists, and has not for some time.**

The README section "The store" (`/home/ted/src/drops.orig/README.md:258`) says:

> A path containing `?` or `#` is rejected. SQLite's `file:` URI parser would silently
> truncate it and open a different database, or drop WAL mode, without any error.

**That is no longer what the code does.** `internal/store/store.go` percent-escapes instead of
rejecting:

```go
func dsn(path string) string {
	escaped := (&url.URL{Path: path}).EscapedPath()
	return "file:" + escaped + "?" + pragmaQuery
}
```

The doc comment records all three original failure modes — `?` truncating the path, `#`
starting a fragment so two different paths collided onto one database, `%` being decoded so
`a%41b.db` opened `aAb.db` — and `url.URL.EscapedPath` handles all three while leaving `/` as
the separator (which `url.PathEscape` would not).

The rejection was removed deliberately. `internal/cli/root_test.go:431` says so:

> `TestURIMetacharacterDBPathOpensEndToEnd` replaces a CLI-side rejection of `'?'` and `'#'`
> in `--db` that existed only as a stopgap while `store.Open` built its DSN by concatenation
> (`br-sht.5`). `store.Open` now percent-escapes the path, so these paths are ordinary paths
> and the CLI must simply use them.

Grepping the whole tree for a surviving rejection finds none.

### The escaping still works — measured, on both drivers

Opening each path and comparing what `PRAGMA database_list` reports against the requested
file **by inode** (the check whose absence caused the original `#` regression):

| path | modernc v1.57.0 | ncruces v0.35.4 |
|---|---|---|
| `weird?path.db` | ok, same inode | ok, same inode |
| `weird#path.db` | ok, same inode | ok, same inode |
| `weird%41path.db` | ok, same inode | ok, same inode |

So: **the underlying `file:` URI parser still behaves the way that forced the workaround** —
`?`, `#` and `%` remain URI-significant and would still truncate or redirect if concatenated
raw. What changed is that drops stopped fighting it. The correct handling is escaping, it is
four lines, and it is portable across both drivers.

**For the rewrite:** carry the `EscapedPath` approach forward, and carry the inode-checking
test with it. Do not reintroduce a rejection, and do not "simplify" the escaping — every one
of those three characters is a silent-corruption bug, and `TestSpacePathActuallyOpens`
documents that merely asserting `journal_mode=wal` is not enough to catch it, because a
silently-truncated database also reports WAL.

---

## What this changes about the store

The driver stays. Five things about its use should change.

1. **Bump to `modernc.org/sqlite v1.57.0`.** Engine-identical to v1.56.0 (same SQLite 3.53.3,
   same `libc v1.74.4`, byte-identical `go.mod`), full suite green under `-race -cpu=1,4`,
   zero regressions. Pin `libc` to whatever the driver pins; never float it.

2. **Add `_defensive=1` to the DSN.** Measured compatible with WAL, triggers, FTS5
   `integrity-check` and `rebuild`; blocks only direct shadow-table corruption. The DSN
   becomes:

   ```
   _pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)
     &_txlock=immediate&_defensive=1
   ```

   Keep `_txlock=immediate`. Its justification in `store.go` is still exactly right, and the
   16-way concurrency test still passes because of it.

3. **Take `context.Context` on every store method, from the first line.** 73 of 80 DB calls
   in the old store are non-context. This is the single largest shape change, it is
   essentially free when writing from scratch, and it is expensive to retrofit.

4. **Replace string-matched error checks with typed codes.** `errors.AsType[*sqlite.Error]`
   plus `sqlite3.SQLITE_CONSTRAINT_UNIQUE` (measured: 2067), deleting the duplicated
   `strings.Contains(err.Error(), "UNIQUE constraint failed")` in `store/deps.go` and
   `service/issues.go`.

5. **Target Go 1.27 and adopt `encoding/json/v2`** for `--json` output and the JSONL mirror,
   with `jsontext` for line-level control. Declare `go 1.27.0`.

And two corrections to carry into the rewrite's planning:

- **There are three FTS5 indexes, not two** — `issues_fts`, `memories_fts`, `comments_fts`.
- **The README's "paths containing `?` or `#` are rejected" is false** and should not be
  copied into the new README. The store escapes them and opens them correctly.

Not recommended: switching to `ncruces/go-sqlite3`. It is a credible, well-maintained,
no-longer-wasm-interpreted driver that is ahead of modernc on Go 1.27 adoption — but it costs
+90% binary size, is 24–58% slower on exactly the FTS5 paths `search` and `recall` use, and
forces the store to abandon plain `sql.Open` to register FTS5 per connection. Revisit only if
drops needs an encrypted VFS or custom FTS5 tokenizers.

---

## Sources

Primary sources only; every version claim is from the module proxy or the project's own
release artefacts.

- Module proxy — `https://proxy.golang.org/modernc.org/sqlite/@latest`, `@v/list`, `@v/*.mod`
- modernc CHANGELOG — <https://gitlab.com/cznic/sqlite/-/raw/v1.57.0/CHANGELOG.md>
- modernc commit range — GitLab compare API, `cznic/sqlite`, `v1.56.0..v1.57.0`
- modernc libc pinning rule — <https://gitlab.com/cznic/sqlite/-/issues/177>
- ncruces releases — GitHub Releases API, `ncruces/go-sqlite3` (v0.34.2 … v0.35.4)
- ncruces wasm2go translation — `go-sqlite3-wasm/v5` README and generated source, module cache
- ncruces FTS5 extension API — `ext/fts5/fts5.go`, module cache
- Go 1.27 release notes — <https://go.dev/doc/go1.27>
- Go 1.26 release notes — <https://go.dev/doc/go1.26>
- Go releases — <https://go.dev/dl/?mode=json&include=all>
- Reference implementation — `/home/ted/src/drops.orig`: `internal/store/store.go`,
  `internal/store/fts.go`, `internal/store/migrations/`, `internal/cli/root_test.go`,
  `README.md`
