# Issue tracker: drops

Issues for this repo live in **drops**, the cross-project tracker this repo builds.
`drops` is on `PATH` at `~/.local/bin/drops`. For how every verb behaves, see
[docs/cli-contract.md](../cli-contract.md); this file is only the tracker
configuration the engineering skills read.

## When a skill says "publish to the issue tracker"

`drops create` in the repo the work belongs to. Capture the printed id.

## When a skill says "fetch the relevant ticket"

`drops show <id> --json`. The user normally passes the id directly.

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
