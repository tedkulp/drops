# Issue tracker: drops

Issues for this repo live in **drops**, the cross-project tracker this repo builds.
There is no GitHub or GitLab remote. `drops` is on `PATH` at `~/.local/bin/drops`.

drops keeps one database for every repository at `~/.drops/drops.db` and works out
which project you mean from the working directory. So **run every command from
inside the repo the work belongs to** and the scoping is automatic. `-P <slug>`
overrides it; `--all-projects` spans every project.

Every command takes `--json` and exits with a stable code. **Prefer `--json` for
anything you parse.** The collection verbs (`list`, `ready`, `blocked`, `search`)
render one terse row per issue. `show` renders a full page: identity, body, every
relation, and the whole comment thread.

## Everyday operations

| Want | Command |
|---|---|
| capture something fast | `drops q "title"` (prints the id, nothing else) |
| create with detail | `drops create "title" -t task -p 2 -l label -d "body"` |
| what can I work on | `drops ready` |
| what is stuck | `drops blocked` |
| read one | `drops show <id>` for a person, `drops show <id> --json` to parse |
| change one | `drops update <id> --title/-d/-p/-t/--status/-A` |
| finish one | `drops close <id> --reason "why"` |
| undo that | `drops reopen <id>` |
| search | `drops search "term"` (titles, descriptions and comment bodies; add `-a` to reach closed issues) |
| how many | `drops count` |
| link | `drops dep add <id> <blocker-id>` |
| refile into another project | `drops move <id>... --to <slug> [--subtree] [--dry-run]` |
| comment | `drops comment add <id> "body" --author <who>` (prints the comment id) |
| read a thread | `drops comment list <id>`, or just `drops show <id>` |
| undo a comment | `drops comment rm <comment-id>` |
| label | `drops label add <id> <label>...`, `drops label rm`, `drops label list [<id>]` |
| note something durable | `drops remember "text" [--global] [--source <skill>]` |
| read the notes | `drops memories [query]`, `drops memory show <id>` |
| projects | `drops project list`, `project add --slug`, `project rename`, `project archive --force` |
| where am I | `drops config show` |
| is the store sound | `drops doctor` |
| talk to the other machine | `drops sync` |

Issue types: `task`, `bug`, `feature`, `epic`, `chore`, `research`, `decision`.
Priority is 0 (critical) to 4 (backlog), default 2.

Every scanning verb answers in one order: priority ascending, then newest first,
then by id. Relevance never reorders a queue you are working through, so a
`search` result and a `list` result put the same two issues in the same order.

## Ids

A new issue id is five random characters and no prefix, so `k3f9x`. A child
created with `--parent` takes a `.N` suffix under its parent, starting at `.1`,
so `k3f9x.3` is the third child of `k3f9x`. A comment id is
`<issue-id>:<12 hex>`, so it carries its own issue and `comment rm` needs no
issue argument.

**A memory keeps a `mem-` prefix; an issue has none.** Ids minted before
2026-08-28 carry a project prefix instead (`beacon-0bl`, `br-6vf`, and beads-era
slugs like `beacon-ci-never-executed-4bhg`), and memories carry project-prefixed
shapes like `gitops-r1w`. **All of these shapes are permanent and equally
valid.** An id is an opaque key: nothing parses it, and `show` resolves any of
them from any working directory. Do not read a project out of an id, and do not
"fix" one.

The alphabet excludes `0`, `1`, `i`, `l` and `o`, so a hand-typed id has no
confusable characters. A mistyped id does not resolve; nothing normalises it.

**An id names an issue or a memory, never both, and the schema is what
guarantees it.** Every minted id is first reserved in `id_owners`, whose `id` is
a primary key over both kinds; `issues` and `memories` each carry a foreign key
on `(id, owner_kind)` back to that row. So the two tables cannot disagree about
who owns a string, and an import that tries to make them is refused as an
identity collision rather than committed and swept up afterwards.

This is worth stating because it used to be a live hazard. The old build let
`drops remember --key <an issue id>` succeed at exit 0, leaving one string
naming two rows, and carried a `doctor` sweep for the wreckage. The custom-key
form is gone with the rest of the memory verbs the usage data did not support,
the reservation makes the collision unwritable, and `doctor` no longer looks for
it.

## Scoping: how a project is resolved

The rules are tried in order, and the first that answers wins:

1. **`-P <slug>`.** A named project. An unknown slug is an error, not a
   fallback.
2. **`$DROPS_PROJECT`.** The same thing from the environment, for a session that
   works one project from many directories. An unknown slug is still an error.
3. **A workspace binding** on this checkout's Git root — the exact directory a
   project was registered from.
4. **This checkout's `origin`**, matched against the projects' repository
   locators. Two projects claiming one origin is reported, never guessed.
5. **The deepest workspace binding containing the working directory**, and only
   when that binding lies inside this directory's repository. An unbound nested
   Git root therefore beats an ancestor binding, and a worktree binding does not
   speak for its repository.
6. **Nothing.** A read reports it; a write registers.

A **read** that resolves to no project prints `drops: no project resolves here;
pass -P <slug> to scope explicitly` on **stderr**, exits **0**, and writes
nothing to stdout. Check stdout, not the exit code. A **write** from an
unregistered Git root registers that repository under a slug derived from its
origin (or its directory name) and files there; a write from outside a
repository files under the reserved `inbox` project. When every candidate slug
is already taken the write is refused rather than suffixed, because a guessed
name would silently collect every later write from that directory.

`--all-projects` spans every project instead of resolving one. `--inbox` scopes
to the reserved inbox. `--db <path>` or `$DROPS_DB` names a different store,
which is how development runs against a scratch database.

**Archiving retires a project from new work; it does not hide its history.**
`drops project archive <slug> --force` drops it from `project list` (`--archived`
brings it back) and makes it refuse new issues and memories. Its existing issues
stay exactly where they are and stay readable: `-P` still scopes to it, and
`--all-projects` still spans it, because `--all-projects` means every project and
an archived one is still a project. The two reserved projects cannot be archived.
`--force` is required from a non-interactive session.

`drops config show` prints the resolved project and store path, and is the
quickest way to find out why a command answered the way it did. It prints
sorted `key=value` lines, or with `--json` one object of the same pairs;
`project.slug` is absent, in both forms, when nothing resolves.

## How a long body gets in

Every text-bearing verb takes its body as an **argument**, so a long or multi-line
body comes from the shell:

```sh
drops create "title" -d "$(cat body.md)"
drops update <id> -d "$(cat body.md)"
drops comment add <id> "$(cat note.md)"
drops close <id> --reason "$(cat answer.md)"
```

**That is the convention, not a gap.** There is no `--description-file`, no
`--body-file`, no `-F`, no stdin form and no `$EDITOR` handoff, and the absence is
a choice. Measured against the binary and the live store on 2026-09-02:

- **One argument caps at 131,071 bytes** on this machine. `ARG_MAX` is 4,194,304
  and irrelevant: Linux caps a *single* argument at 128 KiB independently of the
  total, so `-d "$(cat …)"` hits that limit first and fails loudly with
  `argument list too long` rather than truncating. A 128,000-byte body written
  that way read back byte-identical.
- The longest text anyone has stored is 30,366 bytes (`drops-il6`'s
  description), so the headroom is about 4.3x. That margin is narrower than the
  old doc's 90x claimed, because that figure came off a macOS measurement where
  `ARG_MAX` was the binding limit; here the per-argument cap is. It is still
  four times the longest thing written in the store's life, and hitting it is an
  error you see rather than data you lose.
- drops stores a body **verbatim**. Nothing in the core layer or the CLI trims
  it, so leading whitespace, tabs, `"`, `$` and backticks all survive.
- The `show --json`, edit, `update -d` round trip that the wayfinder map body uses
  is **byte-stable** after the first pass.

One real cost remains. **`$(cat …)` strips trailing newlines**, and no drops change
can fix it, because the loss happens in the shell before drops sees the argument.
`jq -r` adds one newline back, so the round trip converges rather than eroding, and
trailing blank lines are the only thing it can ever drop. Write a body with its
trailing blank lines already gone and the round trip is exact.

**Declined, and why**, so nobody re-proposes them:

- **A bare `-` meaning stdin.** `-d -` today stores a literal `-`. Making it mean
  stdin changes shipped behaviour to buy nothing, because nothing is near a size
  limit.
- **`-F/--file`.** To be worth having it would have to land on every text-bearing
  field: description, close reason and comment body. That is three flags to
  replace an idiom that already works.
- **`$EDITOR`.** The consumer is an agent. An agent writes a temp file without
  friction and never opens an editor.

**What would overturn this.** One body above 131,071 bytes. The longest is 30,366.

## Moving an issue between projects

`drops move <id>... --to <slug>`. **The id never changes.** A new-style `k3f9x` says nothing
about a project to begin with. An older id keeps a project prefix that no longer matches:
`br-6vf` in project `drops` is correct, because that prefix records where the issue was
minted. Either way the issue's project is the only answer to where it lives. Do not
"fix" a mismatched prefix, and do not use one to infer a project.

The destination is `--to`, never `-P`. `-P` is the persistent scoping flag on every verb, so
a `move` without `--to` is refused at exit 2 rather than quietly scoping the lookup and
moving nothing. The destination must already exist; `move` never auto-creates one.

`--subtree` adds every descendant, by the union of the `<parent>.N` id rule and explicit
`parent-child` edges. Every status moves, tombstones included. An issue already in the
destination is a reported no-op, so re-running a half-finished triage is safe. One lost
compare-and-swap abandons the whole move, so a failure means retry, never repair.

The move prints the relationships it made cross a project boundary, `blocks` apart from
`parent-child`. Read the `blocks` list: `ready` and `blocked` take candidates from one
project but compute blockers across the whole store, so a blocker left behind makes an issue
un-ready with no visible cause where it now lives. `blocked` still names the blocker id, and
`show <id>` resolves it from any working directory.

`--dry-run` runs the real write path and rolls it back, printing what a real move would
print. `--json` emits **one object**, not an array: `moved`, `noop`, `crossing_blocks`,
`crossing_parentage`, `to` and `committed`, with the four lists always present as `[]`.
The records in `moved` are the pre-move images, so their `project_key` is the project they
came *from*; `to` is where they went.

**The crossing lists do not follow that rule, deliberately.** A moved endpoint there
reports the project it is going *to*, so `from_project != to_project` on every entry and
an edge whose two ends land in one project is not listed at all. Reporting both ends
pre-move printed the same project on both sides of every entry, which named no boundary
and made the list useless for the one thing it exists for. A move that *joins* two ends —
sending a blocker after the issue it blocks — reports nothing, because nothing crossed.

Issues only. A memory's scope moves with `drops memory edit <id> -P <slug>` or `--global`.

## Comments

`drops comment add <id> "body"` appends a comment and prints its id. The body is
a positional argument, so a long one comes from the shell the same way a
description does. `drops comment add <id> "$(cat note.md)"`. See
[How a long body gets in](#how-a-long-body-gets-in).

**Pass `--author`.** It defaults to the global git `user.name`, else the OS
username, which is right for a human and wrong for an agent. An agent commenting
as the repo's owner is a false record.

**A comment does not touch the issue's `updated_at`,** and that is deliberate.
A comment is its own replicated record with its own revision; bumping the issue
would make every remark on a thread an edit of the issue itself, so two machines
commenting on one issue would manufacture a merge conflict out of nothing. The
cost is that a growing thread does not count as activity in anything ordered by
`updated_at` — order by the comments' own `created_at` when you want the live
threads. A closed issue accepts comments.

**An issue has one body, and the thread holds everything else.** `design`,
`acceptance_criteria` and `notes` were columns inherited from beads that no CLI
verb could ever write. They are gone. The 38 populated `notes`
rows became comments authored `migration`, because every one of them was a dated
after-the-fact update, which is what a comment is; the 5 `acceptance_criteria`
rows folded into the description under an `## Acceptance criteria` heading,
because that is the spec an issue was written against rather than something that
happened later. Use that split when you are deciding where to put text.

`drops search` covers comment bodies as well as titles and descriptions, so
nothing became less findable in the move. A match in a comment is deliberately
indistinguishable from a match in a description: the result is still one row per
issue, in the same priority order.

**There is no `comment edit`, deliberately.** A comment body is a record of what
someone said, and rewriting it silently changes something another reader may
already have acted on. `comment rm` exists for an obvious mistake; a revision is
a new comment.

## Memories

A memory is a durable note that outlives an issue: something learned about a
repository, a workstation, or a workflow. It is an explicit, project-owned
notebook entry, not an ambient recall system — nothing surfaces one for you, so
a memory only helps if someone goes and reads it.

```sh
drops remember "text" [--title "..."] [--source <skill>] [--global]
drops memories [query] [-P <slug>] [-a] [--deleted] [--limit N]
drops memory show <id>
drops memory edit <id> [--title|--body|--source|--global|-P <slug>]
drops supersede <old-id> "the corrected text"
drops forget <id>
```

- **Reads span every project by default**, the inverse of the issue verbs: a
  memory bound to one repo is still findable from another. `-P <slug>` narrows.
- **Writes land where an issue would**, by ordinary scoping. `--global` assigns
  the reserved `global` project instead.
- A title is derived from the body when you do not pass one.
- `supersede` mints the replacement, links the old one to it, and retires the
  old one, all in one transaction. The replacement always inherits the old
  memory's project, so `supersede` has **no `--global`**, and a `-P` naming any
  other project is refused at exit 2 rather than accepted and thrown away.
  `memories` shows only the current end of each chain; `-a` reveals the retired
  links and says what replaced them.
- `forget` tombstones. `memories --deleted` shows only tombstones, marked `⊘`,
  including one that was superseded before it was forgotten. A memory that is
  both retired and removed reports the removal: `⊘ … · tombstoned`, never the
  supersession under it.

**A supersession chain lives in one project**, enforced by the schema. So
`memory edit -P`/`--global` on any link of a chain is refused at exit 2, naming
the other link; correct the text in place, or remember a fresh memory in the
destination.

**Recall, priming, ranking, pinning, kinds and custom keys are gone.** They were
cut on the usage data, not on taste: `recall_count` was 0 across all 149 rows
because `drops recall` had never returned a memory in the store's life.

## When a skill says "publish to the issue tracker"

`drops create` in the repo the work belongs to. Capture the printed id.

## When a skill says "fetch the relevant ticket"

`drops show <id> --json`. The user normally passes the id directly.

## Scanning verbs and the reading verb

`list`, `ready`, `blocked` and `search` are **scanning verbs**: one row per issue,
always exactly one. `show` is the **reading verb**: one issue in full. The split
decides what each may drop, and the rule is not the same in both:

- A scanning verb **truncates what it displays** (a title is a label you recognise)
  and **never what identifies** (an id is a key you paste). `show` truncates nothing.
- **Truncation happens only when stdout is a terminal.** Piped or redirected, the
  full title prints, so `drops list | grep` sees every byte. This deliberately
  differs from `show`, which wraps at a fixed 80 off-terminal: wrapping is lossless
  and truncation is not, so copying the mechanism would have dropped grep matches
  silently.
- The id column **sizes to the widest id in the result set**, so column positions
  vary between invocations. Do not write a parser against a byte offset; use `--json`.
- A **project column appears only under `--all-projects`.** It has to exist, because
  `move` keeps an id byte-identical across a project change, so the prefix does not
  answer which project a row is in.
- Continuation lines (`ready`'s `unblocks N`, `blocked`'s `blocked by a, b`) are
  indented a fixed four columns, and the blocker list is comma-joined.
- All four page through `$PAGER` like `show`. `--json` never pages.

**The memory verbs are outside this.** `memories` and `memory show` do not go
through the renderer at all: they never truncate, never wrap, never page, and
have no width, so a long memory relies on the terminal's own soft wrap. Only the
glyph is shared. That was never decided, only built — see
[Should the memory verbs go through render?](drops://dw32p.33).

There is no assignee column, no label column and no `--wide`. Measured 2026-09-02:
6 of 174 open issues carry an assignee. `show` answers both for a row you picked out.

### What a row looks like

One glyph, the id, the priority, the type padded to eight columns, the project
under `--all-projects`, then the title. A continuation sits four columns in.

```
◐ dw32p.20 P2 task     Build the render package
    unblocks 1
● br-6vf   P0 decision Does cobra still earn its place?
```

The glyph vocabulary is `○` open, `◐` in_progress, `●` closed, and `⊘`
tombstoned, which **outranks** whatever status a tombstoned row carries: a
removed issue reading as open is worse than one whose lifecycle is hidden.
Nothing parses these — every scripted consumer reads `--json`. The same
vocabulary marks a relation on a page and a memory in `memories`, so a listing
and a page can never disagree about what a state looks like.

### What a page looks like

Identity, then body, then relations, then comments — what the issue *is* before
what it is attached to.

```
Build the render package
dw32p.20 · drops · in_progress · task · P2 · @Ted Kulp · wayfinder:task
opened 2026-09-01, updated 2026-09-02

## Question

Build `internal/render` from scratch over `internal/model`.

Parent
  ○ dw32p  Rewrite drops from scratch

Blocked by
  ● dw32p.14  Build the model package

Comments  1

2026-09-02 · agent · dw32p.20:0a1b2c3d4e5f
  measured against the reference build.
```

- The identity strip carries `tombstoned` after the status when the issue is
  tombstoned, `@assignee` when it has one, and the comma-joined labels last.
  Every part is omitted when empty, and the strip wraps like anything else.
- Dates are `opened`, `updated` and, when closed, `closed`, sliced to the date
  from an opaque timestamp that is never parsed and reformatted. A deferral
  gets its own `deferred until` line.
- A close reason prints under `closed: `, wrapped, with continuations hanging
  eight columns. A blank line inside it is left bare rather than indented.
- An **empty description contributes nothing, not a blank line**: a thin issue
  costs three lines. An empty relation block or thread prints no heading.
- `Children` carries its own count, `Children  N, M open`, where a tombstoned
  child is not open whatever its status says.
- A relation title too long for the line **wraps under the id column**. A page
  truncates nothing, relation titles included.
- A body is reflowed **by paragraph**, never line by line: bodies are authored
  hard-wrapped at whatever width their author used, and re-wrapping each source
  line leaves a one-word orphan on alternating lines. Headings, quotes, table
  rows, thematic breaks and anything indented four or more columns are emitted
  untouched — a wrapped command is no longer the command. **A fence is a mode,
  not a line:** ``` or `~~~` suspends every other rule, blank lines included,
  until its matching close, so the contents of a code block survive intact and
  an unclosed fence runs to the end of the body. A list item reflows together
  with its one-to-three-space continuations, hanging two columns. Widths are
  counted in **runes**, so a paragraph of em dashes wraps where it looks like it
  should.

### Width and paging

Off a terminal the wrap is a **fixed 80 columns**, so redirecting `show` into a
file or a diff produces the same bytes on every machine. On a terminal the
measured width is used, capped at 100, because prose past roughly a hundred
columns is measurably harder to read. `$COLUMNS` wins over the measurement, so a
caller can pin the width with or without a terminal — but **truncation** in a
scanning verb is gated on terminal-ness alone, so pinning `$COLUMNS` off a
terminal changes wrapping and never starts truncating.

The renderer does not discover either fact. It never inspects an `*os.File`,
reads `$COLUMNS` or `$PAGER`, or calls ioctl: the CLI measures the terminal
once and states the answer, which is why every byte above is reproducible from a
struct literal in a test.

Paging is `less -FRX` unless `$PAGER` says otherwise; an explicitly empty
`$PAGER` disables it. `-F` exits immediately when the output fits one screen,
so a thin issue still prints inline. A pager that cannot start is not an error —
the output goes straight to stdout instead, because failing to show an issue
because `less` is missing would be the worse bug.

### `--json`

A scanning verb emits a **bare array**, a reading verb a **bare object**, each
followed by exactly one newline and no envelope. An empty result is `[]`, never
`null`, and a nil list *inside* a value is `[]` too, which is what makes
`show --json`'s `.parent`, `.blockers`, `.blocking`, `.children` and `.comments`
safe on every issue rather than on most of them: `parent` is `null` when there
is none, and the four lists are `[]` rather than absent. Nothing is escaped
beyond what JSON requires, so an issue body full of `<`, `>` and `&` stays
readable and stays byte-stable against the mirror. `--json` never pages.

Two shapes to know before you write a parser against it:

- **`show` takes more than one id**, and with several it emits an **array**, not
  an object. `show <id> --json` is the object form; `show <a> <b> --json` is not.
- **`labels` is the one key that goes absent.** It is omitted when an issue
  carries none, so `.labels[]` is not safe the way `.blockers[]` is. Use
  `.labels // []`.

An issue's project appears as **`project_key`**, an opaque immutable key, not as
a slug. Slugs are renameable metadata and keys are what sync agrees on across
machines, so the key is what travels in JSON. `drops project list --json` maps
key to slug; the text renderer prints the slug for a human.

## Sync: the second machine

`drops sync` is one cycle — pull, import, export, push — against a Git-backed
mirror shared with the other machine. It is the only transport: there is no
import subsystem and no other way in.

The transport's files sit beside the store, in `~/.drops/`:

- `mirror.jsonl` — the deterministic JSONL snapshot, the only file that crosses
  machines.
- `replica.json` — this installation's replica key and last export head. Machine-
  local, never replicated. It is minted on the first local write that must be
  exported, and a malformed one is a hard error rather than something silently
  replaced.
- `.sync.lock` — the advisory lock serialising a sync across processes. A sync
  held by another process exits **10**.
- `.git/` — the mirror's history, on branch `main`, pushed to and pulled from
  remote `origin`. drops creates the repository itself; **you add the remote with
  `git`** (`git -C ~/.drops remote add origin <url>`), because drops has no flag
  for it.

A successful cycle prints `exported at <sha>` or `imported N, ignored M`, and
`nothing to sync` when both stores already agree. A fetch it cannot complete —
no remote, a remote it cannot reach — is an error at exit 1, not a quiet no-op.

Merges are deterministic and reported, never interactive. Records merge by
revision, a tombstone beats an equal-generation edit, an opaque id arriving under
a different creation replica is an identity collision that refuses the whole
import, and a snapshot that does not descend from the head this store last
accepted is a fork and is refused before anything commits.

`drops replica rekey` rotates this installation's future identity — after a
restore, or when two installations have provably shared one key. It does not
rewrite history.

## Doctor

`drops doctor` checks the store and exits non-zero on any finding: SQLite
`quick_check`, `foreign_key_check`, and the three external-content FTS indexes
(`issues_fts`, `comments_fts`, `memories_fts`) against their source tables.

- `--repair` rebuilds any FTS index that failed. It repairs nothing else; a
  quick-check or foreign-key finding is reported for a person to look at.
- `--scan` reads every stored issue and memory for credentials and flags what it
  finds. It **never redacts** — it names the record and the field and leaves the
  text alone.

It is offline and store-wide: it takes no project scope and talks to no network.

## Exit codes

| Code | Means |
|---|---|
| 0 | fine |
| 1 | it crashed |
| 2 | you called it wrong: a bad flag, a bad positional count, an unknown verb or subcommand, a refused write |
| 4 | a named issue, memory or project does not exist |
| 5 | a conflict: a lost compare-and-swap, or a supersede against an already-retired memory |
| 10 | a sync lock held by another process |

Codes 3 and 6 through 9 belonged to the `bd` compatibility face and the
migration subsystem, both cut. **A mistyped verb is 2, not 0** — the old build
printed a group's help and exited 0, so `drops comment edit <id> "..."` reported
success having written nothing.

**Two diagnostic verbs overload 1.** `drops doctor` and `drops dep cycles` exit
1 when they *find* something, which is a report, not a crash. Read their stdout:
both name what they found. Every other verb's 1 means what the table says.

## Known rough edges

So a session does not mistake them for its own error:

- **`show --json` carries relations and comments; `list --json` does not.**
  `show` emits `parent`, `blockers`, `blocking`, `children` and `comments`
  alongside the issue's own fields. No other verb emits them. `comments` carries
  whole bodies, so `show --json` on a long thread is large.
- **`list --json` omits `labels`.** Filtering a `list` result by label in your
  own code therefore matches nothing, silently. Use the server-side `-l` /
  `--label-any` flags, which work correctly, or fetch the issue with
  `show --json` when you need its labels.
- **`list --parent <id>` misses migrated children.** It matches on the id
  (`<parent>.N`) and ignores the `parent-child` records, of which cutover writes
  549; it is also scoped to the current project, so a child that was moved
  elsewhere is invisible. `drops show <id> --json | jq -r '.children[].id'` takes
  the union of both and is the reliable answer.
- **A scanning verb exits 0 with an advisory on stderr when the cwd resolves to
  no project.** Check stdout, not just the exit code.
- **`blocked --json` is not a flat array of issues.** Each element is
  `{"issue": {...}, "blocked_by": [...]}`, which is deliberate (it carries the
  blockers), but it means `.[].id` works on `list` and `ready` and returns nothing
  on `blocked`. Use `.[].issue.id` there. **`blocked_by` is a bare array of id
  strings**, not of issue objects, so `.blocked_by[].id` fails with `Cannot index
  string with string`. `.blocked_by | join(", ")` and `| length` are the two useful
  forms.
- **Deferral is readable and not writable.** `list --deferred` filters on
  `deferred_until` and `show` prints a `deferred until` line, but no verb sets
  one: the data is preserved from the old store, and deferral in practice is done
  with a label. Do not plan a workflow around setting it.

`drops label list` **used** to ignore `-P` and answer store-wide (`i-wnkh7`).
The rewrite scopes it like every other read, so a per-project label count is now
that project's numbers.

## Wayfinding operations

Used by `/wayfinder`. The **map** is one issue; its **tickets** are that issue's
children.

**Map.** An `epic` labelled `wayfinder:map`, created in the repo the effort
belongs to:

```sh
drops create "<effort name>" -t epic -l wayfinder:map -d "$(cat body.md)"
```

The body is the Destination / Notes / Decisions-so-far / Not-yet-specified /
Out-of-scope document. It arrives through `-d "$(cat body.md)"`, per
[How a long body gets in](#how-a-long-body-gets-in). To amend it, read
`drops show <map> --json`, edit the `description`, and write it back with
`drops update <map> -d "$(cat …)"`.

**Ticket.** A child of the map, carrying its type label:

```sh
drops create "<question title>" --parent <map-id> \
  -l wayfinder:research   # or wayfinder:prototype | wayfinder:grilling | wayfinder:task
  -d "## Question

<the decision this ticket resolves>"
```

Leave tickets at the default type `task`. That is load-bearing: it is what keeps
the map itself out of the frontier query.

**`--parent` carries the project, and the working directory is not consulted.**
A ticket lands in its map's project wherever you run `create` from, so a session
working a map in another repo no longer has to stand in the right directory. No
project is resolved at all on that path, which also means `create --parent` can
no longer auto-register a project for an unregistered cwd.

This was the reverse until `drops-il6.13`, and the reversal is why the rule is
worth stating rather than assuming: `beacon-p9d.1` through `.9` were created
against a map in `beacon`, landed in `inbox`, and all nine were deleted and
re-created as `.11` through `.19`, burning ten ids. Those nine are the only
straddling parent-child pairs the store has ever held, they are all `deleted`,
and no repair was made.

Two refusals come with it:

- **A `-P` or `--inbox` that disagrees with `--parent` is an error**, exit 2,
  naming both projects. One that agrees is fine. The cwd is ambient and carries
  no intention, so a parent silently overrides it; `-P` is a typed argument, so
  a contradiction cannot be resolved by guessing.
- **A `--parent` naming no issue is an error**, exit 4. It used to succeed:
  scanning for `<parent>.%` alone minted `zzzzz.1` under an issue that did not
  exist. Inheriting the project made the parent lookup mandatory, which is what
  turned this into a refusal.

`drops move` is deliberately unchanged and still allows a subtree to be split
across projects, reporting what it crossed. That asymmetry is intended:
`create --parent` never carried an intention about projects, so inferring one is
free, while `move --to` is nothing but an intention about projects.

**Blocking.** Native dependencies, so the tracker computes the frontier itself:

```sh
drops dep add <blocked-id> <blocker-id>     # <blocked-id> depends on <blocker-id>
```

`drops dep tree <id>` shows the longest chain of open blockers; `drops dep cycles`
lists every cycle and exits non-zero when there is one. **`dep add` does not
refuse a cycle** — it records the edge and leaves `dep cycles` to find it — so a
map whose blocking edges you wired in a second pass is worth running it against.

**Frontier.** Open, unblocked, unclaimed children of the map:

```sh
drops ready -t task --json | jq --arg m "<map-id>." \
  '[.[] | select(.id | startswith($m)) | select(.assignee == null)]'
```

`drops ready` already means open and unblocked. `-t task` drops the map's own
epic, which is otherwise unblocked and shows up in its own frontier. The
`assignee` filter is done here because `list` and `ready` have no assignee flag,
and it works on an absent key as well as a null one. First by id order wins.

**Claim.** Before any work, so concurrent sessions skip the ticket:

```sh
drops update <ticket-id> -A "<dev name>"
```

An open, unassigned child is unclaimed. The assignee *is* the claim.

**Resolve.**

```sh
drops close <ticket-id> --reason "<the answer>"
```

The reason is stored as `close_reason` and is readable via
`drops show <id> --json`. Then append a one-line gist plus the ticket id to the
map's Decisions-so-far, using the `update -d` round-trip above.

**A resolution goes in `close --reason`, not in a comment.** This is a choice,
not the absence of an alternative: comments exist and resolutions stayed here
anyway. 836 issues already carry a `close_reason`, and `show` renders it under
`closed:` where a reader looks for why an issue ended. A comment carries no
ordering relative to the close, so putting the answer there makes it harder to
find. Comments are for what accumulates while a ticket is open.

The one cost, so it is not a surprise: `close_reason` is a single column, so
reopening and re-closing a ticket overwrites it, where a thread would
accumulate.

**Rule out of scope.** Close the ticket without a decision reason:

```sh
drops close <ticket-id> --reason "out of scope: <why>"
```

and record it under the map's Out-of-scope section rather than Decisions-so-far.
