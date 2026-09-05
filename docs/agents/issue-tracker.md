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
| browse interactively | `drops tui` (a human at a terminal; never an agent) |
| change one | `drops update <id> --title/-d/-p/-t/--status/-A` |
| claim unassigned work | `drops claim <id> <who>` |
| release a claim | `drops release <id>` |
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
| why did it route there | `drops config show` for this directory, `drops project list --json` for the store |
| is the store sound | `drops doctor` |
| talk to the other machine | `drops sync` |
| which binary is this | `drops version` |

Issue types: `task`, `bug`, `feature`, `epic`, `chore`, `research`, `decision`.
Priority is 0 (critical) to 4 (backlog), default 2.

**`ready` and `blocked` partition `list`.** Both take their candidates from the
same live set `list` shows — `open` **and** `in_progress` — and split it on one
question: does this issue have an unfinished blocker? So every live issue is in
exactly one of them, and `ready` shows the work you have already started rather
than hiding it. Neither reaches a closed issue, which is why `-a` is refused on
both rather than ignored. A deferred issue is the one exception to the sum: it
leaves both until its deferral passes.

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

### Why did it answer that way

Two verbs read back what the ladder decided from, and between them they cover
both sides of it: what this directory looks like, and what the store holds.

`drops config show` answers for **the current directory**. It prints sorted
`key=value` lines, or with `--json` one object of the same pairs:

| key | what it says |
|---|---|
| `store.path` | the database this invocation opened |
| `project.slug` | the project that resolved |
| `project.rule` | the rung that answered: `project-flag`, `env-project`, `workspace-binding`, `repository-locator`, `workspace-prefix`, `inbox-fallback` |
| `git.root` | the Git root rung 3 compares against the bindings |
| `git.origin` | this checkout's origin **normalized**, which is the form rung 4 matches |

Every key but `store.path` goes **absent** rather than empty when there is
nothing to say — outside a repository there is no `git.root`, and where nothing
resolves there is no `project.slug` and no `project.rule`. `git.origin` is worth
one caution: `git remote get-url origin` prints the raw URL and the ladder never
compares that, so this is the one fact here you cannot reproduce by hand.

Two details make it usable as a diagnostic rather than only as a report:

- **The `git.*` keys describe the directory, not the scope.** `--inbox` and
  `-P` name a project without the directory having answered, and the Git facts
  are reported either way. Under `--inbox` no rung runs, so `project.rule` is
  the key that goes absent.
- **It prints what it has before it refuses.** Where two projects claim one
  origin, `config show` writes its `key=value` lines (or its object) to
  **stdout** and *then* exits **5** with the collision on stderr. That case is
  the reason the verb exists, so a bare refusal would be the one question it
  could not answer.

`drops project list --json` answers for **the store**. Each project carries the
two tables the ladder consults, and neither key ever goes absent — a project
with none answers `[]`:

```
.workspace_bindings[]   { "path", "project_key" }
.repository_locators[]  { "project_key", "locator", "tombstoned", "revision" }
```

**Only one of the two travels.** A repository locator is a replicated record: it
carries a `revision`, it moves in a `sync`, and every machine agrees on it. A
workspace binding is **machine-local** — one machine's absolute path, no
revision, in no snapshot — so a binding you can see here is not one the other
machine has. The presence of `revision` on a row is what says which is which.
Bindings hang off the project they name, so a binding to an archived project is
only visible under `project list --archived`.

The two together are what a routing question needs. A directory that does not
route, on a store where the project is plainly listed, is normally a binding
holding **the other machine's path** with no locator to catch the miss at rung 4
— `config show` gives you this machine's Git root and origin, `project list
--json` gives you what they were compared against.

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

## Shell completion

`drops completion <bash|zsh|fish>` prints the completion script for that shell to
stdout. It does not open the store, so it works before the database exists. An
unknown shell is refused at exit 2. PowerShell is not offered: drops is
UNIX-only (`flock`), and a generated script nobody here can run is the dead
weight dw32p.1 measured rather than a feature.

Installation is a one-line source, and this is the step that decides whether
the feature repeats dw32p.1's history (no script installed anywhere):

```sh
source <(drops completion bash)                 # bash
source <(drops completion zsh)                  # zsh
drops completion fish | source                  # fish
```

For a persistent install, write the script where the shell's framework looks:
bash reads `~/.local/share/bash-completion/completions/drops`, zsh reads a
`_drops` file on its `fpath`, fish reads `~/.config/fish/completions/drops.fish`.

**What completes.** Two halves, costed separately because only one touches the
store. The static half — verb and flag names, `--type`'s seven types,
`--status`, `--priority` 0–4 — costs no store read. The dynamic half costs one:
issue ids, project slugs, and existing labels.

**Issue ids are the value, and they complete as a picker, not a prefix matcher.**
Cobra's `id\tTitle` form carries the title as the description, so a tab press
over the current project's issues reads like a menu rather than a list of
random strings. The scope is `list`'s: the working directory's project, `open`
**and** `in_progress`, widened only by the same flags — `-a` adds closed,
`--all-projects` spans every project, `-P` scopes elsewhere. `reopen` is the
one exception: it completes **closed** issues, because a live-only default
would list nothing the verb can act on. Every verb that takes an issue id uses
the same completion: `show`, `update`, `close`, `reopen`, `claim`, `release`,
`comment add`, `comment list`, `dep add/rm/tree`, `label add/rm/list`, `move`,
plus the `--parent` flags.

**The other dynamic values follow their verbs.** `-P/--project` completes every
slug, archived included, because `-P` still scopes to an archived project.
`move --to` completes only live slugs, because an archived project refuses new
work and is never a legal destination. `-l/--label` and `--label-any` complete
the labels that already exist in the selected scope.

This is a human affordance, like `drops tui`. Agents use `--json` and never
tab-complete, so nothing here changes the machine surface.

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

## Scanning and reading verbs

`list`, `ready`, `blocked`, `search` and `memories` are **scanning verbs**: one
row per result, always exactly one. `show` and `memory show` are **reading
verbs**: one issue or memory in full. The split decides what each may drop, and
the rule is not the same in both:

- A scanning verb **truncates what it displays** (a title is a label you recognise)
  and **never what identifies** (an id is a key you paste). A reading verb
  truncates nothing.
- **Truncation happens only when stdout is a terminal.** Piped or redirected, the
  full title prints, so `drops list | grep` sees every byte. This deliberately
  differs from a reading verb, which wraps at a fixed 80 off-terminal: wrapping
  is lossless and truncation is not, so copying the mechanism would have dropped
  grep matches silently.
- The id column **sizes to the widest id in the result set**, so column positions
  vary between invocations. Do not write a parser against a byte offset; use `--json`.
- An issue row's **project column appears only under `--all-projects`.** It has
  to exist because `move` keeps an id byte-identical across a project change, so
  the prefix does not answer which project a row is in. Memory rows have no
  project column.
- Issue continuation lines (`ready`'s `unblocks N`, `blocked`'s `blocked by a,
  b`) are indented a fixed four columns, and the blocker list is comma-joined.
- Every scanning and reading verb pages through `$PAGER`. `--json` never pages.

There is no assignee column, no label column and no `--wide`. Measured 2026-09-02:
6 of 174 open issues carry an assignee. `show` answers both for a row you picked out.

### What a row looks like

An issue row is one glyph, the id, the priority, the type padded to eight
columns, the project under `--all-projects`, then the title. A continuation sits
four columns in.

```
◐ dw32p.20 P2 task     Build the render package
    unblocks 1
● br-6vf   P0 decision Does cobra still earn its place?
```

A memory row uses the same glyph and measured id column, then its title and any
retirement state. It has no priority, type or project columns:

```
○ mem-k3f9x   Current notebook entry
⊘ gitops-r1w  Removed notebook entry · tombstoned
```

The glyph vocabulary is `○` open, `◐` in_progress, `●` closed, and `⊘`
tombstoned, which **outranks** whatever status a tombstoned row carries: a
removed issue reading as open is worse than one whose lifecycle is hidden.
Nothing parses these — every scripted consumer reads `--json`. The same
vocabulary marks a relation on a page and a memory in `memories`, so a listing
and a page can never disagree about what a state looks like.

### What a page looks like

An issue page is identity, then body, then relations, then comments — what the
issue *is* before what it is attached to.

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

`memory show` has its own compact page: an id, title, optional provenance and
retirement state on the header, then the body. It follows the same lossless
wrapping and paging rules:

```
mem-k3f9x · Current notebook entry · from wayfinder
The memory body.
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
- An issue description or memory body is reflowed **by paragraph**, never line by
  line: bodies are authored hard-wrapped at whatever width their author used,
  and re-wrapping each source line leaves a one-word orphan on alternating
  lines. Headings, quotes, table rows, thematic breaks and anything indented
  four or more columns are emitted untouched — a wrapped command is no longer
  the command. **A fence is a mode, not a line:** ``` or `~~~` suspends every
  other rule, blank lines included, until its matching close, so the contents of
  a code block survive intact and an unclosed fence runs to the end of the body.
  A list item reflows together with its one-to-three-space continuations,
  hanging two columns. Widths are counted in **runes**, so a paragraph of em
  dashes wraps where it looks like it should.

### Width and paging

Off a terminal the wrap is a **fixed 80 columns**, so redirecting a reading verb
into a file or a diff produces the same bytes on every machine. On a terminal the
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
key to slug; the text renderer prints the slug for a human. That JSON also
carries each project's `workspace_bindings` and `repository_locators` — the
routing data, described under [Why did it answer that
way](#why-did-it-answer-that-way).

## The navigator: `drops tui`

`drops tui` opens a full-screen two-pane navigator over the current project: the
live issues on the left, one issue in full on the right, and the right pane
retargetable **without the list cursor moving**. It is for a human at a
terminal. **An agent should not run it**: it takes over the screen, reads keys,
and emits no parseable output. Every question it answers has a verb above that
answers it on stdout.

**It refuses to start where no project resolves, and exits 2**, which is the one
place this verb is deliberately asymmetric with `list`, `ready` and `config
show`. Those print `drops: no project resolves here` to stderr and exit 0
because that advisory stays on the terminal; under an alt screen it would be
invisible, so exiting 0 would put a blank pane in front of you with no
explanation. `-P <slug>` scopes it like every other verb.

**What the left pane lists** is `list`'s contract — every live issue in the
scope, `open` **and** `in_progress` — in the same order every scanning verb
uses, never re-sorted. Not `ready`: a pane you navigate from has to show the
blocked rows too, and it annotates them rather than dropping them.

The row is the CLI's row with two changes and one drop, and they are worth
knowing because **this is the one surface in drops that truncates an id**:

- the id column auto-sizes to the widest id present but **caps at 12 columns**,
  cutting past it with the same `…` a title gets;
- the project column, shown only under `a`, caps the same way;
- the type column is dropped;
- a blocked row carries a compact ` [N]` after the title, appended after the cut.

The rule everywhere else — truncate what a row *displays*, never what
*identifies* — is not broken so much as relocated: the detail pane's border
carries the **full untruncated id**, one glance away.

| key | what it does |
|---|---|
| `j` `k` `↓` `↑` `g` `G` `^d` `^u` | move the list cursor; the detail pane follows |
| `/` | filter the loaded rows: a case-insensitive literal substring over id and title |
| `Esc` | go back one layer. It never quits |
| `C` | include closed issues |
| `a` | span every project, and add the project column |
| `r` | ready only: hide the rows with an open blocker |
| `R` | re-read the store |
| `f` | pick a relation of the right pane's issue and follow it |
| `Backspace` | back one relation; `Esc` drops the whole trail |
| `x` | write to the right pane's issue: close, reopen, comment, priority, claim, release |
| `y` | copy the right pane's issue id to the system clipboard |
| `J` `K` `space` `b` | scroll the detail pane vertically |
| `h` `l` `←` `→` `0` `$` | scroll it horizontally, eight columns a step |
| `w` | soft-wrap the detail pane instead of clipping |
| `enter` | drop the two columns for the issue text alone, and back |
| `?` | the keymap. It captures every key while it is up; `?` or `Esc` closes it |
| `q` `Ctrl-C` | quit |

The filter **survives `a` and `C`**, because "filter, then widen the scope to
see if it exists elsewhere" is the motion `a` exists for. `drops search` is a
different thing and has no key: it reaches descriptions and comment bodies and
returns rows outside the current scope.

**`r` hides the blocked rows**, leaving what you can actually pick up, and the
footer says `ready` while it is on. It stacks with `/`, `a` and `C` and
survives all three, because it is a predicate over the rows already loaded —
`blockedBy == 0`, the same count the ` [N]` marker prints — and not a different
question asked of the store.
If that hides every row, the empty pane says
`No ready issues · r to show blocked` instead of reporting the scope as having
no open issues.

So it is **not** `drops ready`, on purpose. Re-asking the store would throw
away whatever the pane's own modes had put on screen: the closed rows `C`
added, and a deferred issue, which `drops ready` omits. Under `r` the row set
stays `list`'s contract and what leaves is the blocked rows, nothing else.

**`Esc` is the universal go-back key, and it pops exactly one layer a press**,
in this order:

1. an open picker — cancel it;
2. the `/` prompt while you are still typing — cancel the typing and the filter;
3. the help screen — close it. The help **captures every key while it is up**,
   so nothing acts invisibly behind it: without that, `x` opened a picker under
   the help and that picker then swallowed the `Esc` meant to close it;
4. the follow trail — drop **all** of it at once, back to the cursor's own
   issue, without the cursor moving;
5. the filter — clear it;
6. `enter`-zoom — leave it.

With nothing live it does nothing. **It never quits**, at any depth: overloading
it to exit means a mistyped `/` drops you out of a full-screen program. One
layer a press rather than all of them, because a key that discarded the filter,
the trail and the zoom together on one stray press is not a go-back key.

**`f` follows a relation without moving the list cursor**, which is the whole
reason this verb exists. It opens a picker over the right pane's issue in place
of the page — grouped under the same headings a page prints, in the same order,
`Parent · Blocked by · Blocks · Children · Discovered from · Discovered ·
Related` — and `Enter` takes the one under the cursor. `j` `k` `g` `G` walk it,
`Esc` cancels it. On an issue with no relations it opens nothing and says so.

The picker's boundary is **wider than `show`'s**: it lists all three dependency
types, where a page renders `blocks` alone. That is not a second opinion about
what a relation is — it is an omission on `show`'s side, filed as
[drops://hxedy](drops://hxedy), and until it is fixed the navigator is the only
place a `discovered-from` edge can be read back at all. Its id column is
auto-sized and **uncapped**, unlike the left pane's: a picker's rows are
siblings and dotted ids share a prefix, so a cap would render seven of one
issue's sixteen children identically.

Following leaves the row set on purpose — `C` governs the left pane only, and
half of all relation targets are closed. While you are away from the cursor's
own issue the right pane carries a `← <where you came from> ·<depth>` line.
**`Backspace` walks back down it one level at a time; `Esc` drops the whole
trail in one press.** Moving the list cursor clears it too, but only by moving
the cursor — which is why `Esc` earns its place: it is the only way back to the
cursor's own issue that leaves the cursor on it.

**Horizontal scrolling is load-bearing, not a nicety.** A page truncates
nothing, including the table rows and fenced blocks it deliberately does not
reflow, so a pane narrower than a line clips it — 43% of open issues emit a
line too wide for the pane an 80-column terminal gives them. The clipped bytes
are off-screen and reachable, and the footer's `↔ 49–86 of 130` says so, which
is what stops a clipped table row reading as a whole row. `w` is the other
answer where alignment does not matter.

The footer is the disclosure line: scope, count, then active modes **as words**
(`ready`, `closed`, `wrap`, `zoom detail`, `stale`, and the filter). The count reads
`M of N` while something is narrowing and a bare `N issues` otherwise — `C` is
a 6.3x row jump in a project this size, and that has to be visible.

**The pane notices when somebody else writes, and does not act on it.** Every
two seconds it asks the store for `write_seq`, the counter bumped inside the
same transaction as every replicated write — so another terminal, another
agent, or a `sync import` all move it, and none can be missed. That ask is one
primary-key row, measured at **4.2µs** against the 1025-issue corpus, where a
whole re-read is 5–10ms; the poll is 1/1300th of the refresh it decides not to
make.

What it does with the answer is put `stale · R` on the footer, naming the key
that clears it. It does **not** reload: this store's common writer is an agent,
and a pane that reordered itself under you mid-thought is the thing the
navigator exists to avoid. `R` is what applies it — unconditionally, whether or
not the marker is up, because an override that silently declines is the key you
press twice. `R` is a re-read and not a navigation, so the follow stack
survives it.

A refresh keeps everything you were doing: the cursor rides its tracked id
wherever the row moved, the filter and the follow stack stand, and **the detail
pane holds your scroll position**. The page you are reading is re-rendered and
compared byte for byte; identical means the viewport is not touched at all, and
different means new text with your offsets carried over.

What decides that is **what is on screen, not which key you pressed**: your
place is kept while the same issue stands there at the same pane width, and a
page that is a different issue — or the same issue reflowed to a new width —
starts at the top. So `/`, `C`, `a` and a taller window leave you where you
were, while `enter`-zoom, the `Esc` out of it and a wider window start you at
the top, because the page has been re-cut into different lines. A refresh that
finds the issue you were reading closed elsewhere moves the cursor to the next
one and opens **that** at the top: it is a page you never scrolled, whatever
motion put it there.

**`x` is the whole write set**, behind one modal on whatever the right pane is
showing. Nothing is bound at the top level, and a row appears only where it
applies: `close` on live work, `reopen` on a closed issue, `comment` and
`priority` always, `claim` on unclaimed live work and `release` on claimed live
work. `priority` opens a second picker, `P0` through `P4`. A tombstoned issue
gets no rows at all and `x` says so, because every write refuses one.

`claim` and `release` are withheld from a **closed** issue even though the
store would allow them: a claim on closed work is the durable record of who
resolved it, and `release` would erase that in one keypress.

**`close` and `comment` open `$VISUAL`, then `$EDITOR`, then `vi`**, on an
empty `.md` temp file — no seeded content, nothing stripped. An empty or
whitespace-only file writes nothing and says nothing; a non-zero editor exit
(`:cq`) writes nothing and says so. That makes `close` stricter than the CLI,
where `--reason` is optional. A comment is attributed to the same name `comment
add` defaults to, and there is deliberately **no `--author` flag**: an agent
cannot drive a full-screen program, so the writer is a human at a keyboard.
`git config --global user.name` is where that name comes from.

**`reopen` asks first**, and it is the only thing that does. Reopening clears
`close_reason` — one column, so the reason is gone — and the prompt states how
many characters it is about to discard. Any key but `y` answers no.

Above the footer sits **one message line**, for a refused write and for the
credential scan. That scan normally writes to stderr, which a full-screen
program makes invisible, so under `tui` it is redirected onto the frame. The
line costs a pane row only while it is up, and the next keypress clears it.

**`y` copies the right pane's issue id** to the system clipboard. The pane's
issue and not the row under the cursor, which is the same thing until you
follow a relation and, under a trail, is the issue you are reading — the object
`x` already names, so the two keys act on one issue and the trail does not
silently change what a key means.

It goes out as **OSC 52**, so it needs no `xclip` or `pbcopy` and works over
ssh. The honest limit is that OSC 52 is not universally supported and the
terminal never acknowledges it: it either takes the sequence or ignores it, and
`drops` cannot tell which. So the footer reports what was **sent** — `sent
<id> to the clipboard` — and not what your clipboard now holds. The notice
lives until the next keypress, and on an empty row set there is nothing on the
right to name, so `y` sends nothing and says nothing.

Writes change the row set, and the cursor's id-tracking is what makes that
readable: closing the cursor's issue with `C` off drops its row and lands the
cursor on the next one, so `x`-close walks the list; changing priority
re-orders the list and the cursor rides its row to the new position. Writing to
a **followed** issue leaves the left pane untouched.

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
  replaced. **`drops tui` mints it on startup** rather than on its first `x`,
  which is the one exception: it is a verb that writes, and once the alt screen
  is up there is nowhere to report a failure to mint — the same reason it
  refuses to start where no project resolves.
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

## Which binary am I driving?

`drops version` — and `drops --version`, which prints the same line — answers in
one line:

```
drops v0.1.0 (e4a8f4b, built 2026-09-03T10:24:20Z)
```

The release, the commit it was built from, and when. A binary built from a tree
with uncommitted changes says so:

```
drops v0.1.0 (e4a8f4b modified, built 2026-09-03T10:24:20Z)
```

**`modified` is the one to read.** `drops` on `PATH` is a *copy*, not a symlink to
a repository, so it does not change when the source does — the version line is
what tells you how stale it is, and `modified` says it was built from a tree that
had work not yet committed.

Each fact is **omitted when it is not known**, never reported as unknown, so a
build made outside the recipe still answers truthfully:

| Line | Built by |
|---|---|
| `drops v0.1.0 (e4a8f4b, built …)` | `just build` or `just install` |
| `drops devel (e4a8f4b)` | `go build` — the commit is embedded by the toolchain, no date is stamped |
| `drops devel` | a build with no repository behind it |

The commit and `modified` are not stamped: Go embeds `vcs.revision` and
`vcs.modified` in every binary it builds from a repository, so they cannot drift
from the source they came from. Only the version and the build date are passed in
by the recipe, the build date because Go records the *commit's* time and never the
build's.

`version` never opens the store, so it answers when the database is the thing
that is broken.

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
  '[.[] | select(.id | startswith($m)) | select((.assignee // "") == "")]'
```

`drops ready` already means live and unblocked, `in_progress` included, so a
ticket someone started and released comes back to the frontier. `-t task` drops
the map's own epic, which is otherwise unblocked and shows up in its own
frontier. The
`assignee` filter is done here because `list` and `ready` have no assignee flag.
It treats both an absent assignee and the historical empty string as unclaimed.
First by id order wins.

**Claim.** Before any work, so concurrent sessions skip the ticket:

```sh
drops claim <ticket-id> "<dev name>"
```

An open child with no assignee, or with a historical empty assignee, is
unclaimed. `claim` refuses to overwrite a non-empty claim, so taking work from
another session is never implicit.

**Release.** When a session stops without resolving its ticket:

```sh
drops release <ticket-id>
```

`release` writes a null assignee, putting the ticket back in the frontier.
`update -A` remains an exact field edit; it is not the claiming operation.

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
