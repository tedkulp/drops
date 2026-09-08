# Issue tracker: drops

Issues for this repo live in **drops**, a cross-project issue tracker for AI
coding agents: <https://github.com/tedkulp/drops>. It is a local binary on
`PATH`, not a forge — an issue has an id and no URL, and `gh` and `glab` play no
part in the workflow below whether or not this repo has a remote.

drops keeps one database for every repository at `~/.drops/drops.db` and works out
which project you mean from the working directory. So **run every command from
inside the repo the work belongs to** and the scoping is automatic. `-P <slug>`
overrides it; `--all-projects` spans every project.

Every command takes `--json` and exits with a stable code. **Prefer `--json` for
anything you parse.** The collection verbs (`list`, `ready`, `blocked`, `search`)
render one terse line per issue. `show` renders a full page: identity, body,
every relation, and a comment count.

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
| search | `drops search "term"` |
| link | `drops dep add <id> <blocker-id>` |

Issue types: `task`, `bug`, `feature`, `epic`, `chore`, `research`, `decision`.
Priority is 0 (critical) to 4 (backlog), default 2.

Ids are `<prefix>-<rand>`; a child created with `--parent` takes a `.N` suffix
under its parent, so `drops-a1b.3` is the third child of `drops-a1b`.

## When a skill says "publish to the issue tracker"

`drops create` in the repo the work belongs to. Capture the printed id.

## When a skill says "fetch the relevant ticket"

`drops show <id> --json`. The user normally passes the id directly.

## Wayfinding operations

Used by `/wayfinder`. The **map** is one issue; its **tickets** are that issue's
children. Everything below was verified against the binary on 2026-08-28.

**Map.** An `epic` labelled `wayfinder:map`, created in the repo the effort
belongs to:

```sh
drops create "<effort name>" -t epic -l wayfinder:map -d "$(cat body.md)"
```

The body is the Destination / Notes / Decisions-so-far / Not-yet-specified /
Out-of-scope document. There is no `--description-file`, so pass the body through
`-d "$(cat …)"`. To amend it, read `drops show <map> --json`, edit the
`description`, and write it back with `drops update <map> -d "$(cat …)"`.

**Ticket.** A child of the map, carrying its type label:

```sh
drops create "<question title>" --parent <map-id> \
  -l wayfinder:research   # or wayfinder:prototype | wayfinder:grilling | wayfinder:task
  -d "## Question

<the decision this ticket resolves>"
```

Leave tickets at the default type `task`. That is load-bearing: it is what keeps
the map itself out of the frontier query.

**Blocking.** Native dependencies, so the tracker computes the frontier itself:

```sh
drops dep add <blocked-id> <blocker-id>     # <blocked-id> depends on <blocker-id>
```

`drops dep tree <id>` shows the longest chain of open blockers; `drops dep cycles`
exits non-zero if any cycle exists.

**Frontier.** Open, unblocked, unclaimed children of the map:

```sh
drops ready -t task --json | jq --arg m "<map-id>." \
  '[.[] | select(.id | startswith($m)) | select(.assignee == null)]'
```

`drops ready` already means open and unblocked. `-t task` drops the map's own
epic, which is otherwise unblocked and shows up in its own frontier. The
`assignee` filter is done here because `list` and `ready` have no assignee flag.
First by id order wins.

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

**Rule out of scope.** Close the ticket without a decision reason:

```sh
drops close <ticket-id> --reason "out of scope: <why>"
```

and record it under the map's Out-of-scope section rather than Decisions-so-far.

**Known rough edges**, so a session does not mistake them for its own error:

- There is no comment verb. The `comments` table exists and holds migrated data,
  but no CLI reaches it, so a resolution comment goes in `close --reason`.
  `drops show <id>` reports the count and the newest line, which is how you tell
  a thread is there at all; reading a whole thread still needs SQL.
- **`show --json` carries relations; `list --json` does not.** `show` emits
  `parent`, `blockers`, `blocking` and `children` alongside the issue's own
  fields, with a shape that does not vary: `parent` is `null` when there is
  none, and the three lists are `[]` rather than absent, so `.blockers[].id` is
  always safe. No other verb emits them.
- **`list --parent <id>` misses migrated children.** It matches on the id
  (`<parent>.N`) and ignores the `parent-child` dependency rows, and 124 of the
  484 such rows in the store have a child id that does not follow that pattern.
  `drops show <id> --json | jq -r '.children[].id'` takes the union of both and
  is the reliable answer.
- There is no `--description-file`; use `-d "$(cat …)"`.
- `drops ready` exits 0 with an advisory on stderr when the cwd resolves to no
  project. Check stdout, not just the exit code.
- **`blocked --json` is not a flat array of issues.** Each element is
  `{"issue": {...}, "blocked_by": [...]}`, which is deliberate (it carries the
  blockers), but it means `.[].id` works on `list` and `ready` and returns nothing
  on `blocked`. Use `.[].issue.id` there.
- **`list --json` omits `labels`; `show --json` includes it.** Filtering a `list`
  result by label in your own code therefore matches nothing, silently. Use the
  server-side `-l` / `--label-any` flags, which work correctly on both, or fetch
  the issue with `show --json` when you need its labels.
