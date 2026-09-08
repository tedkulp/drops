# Using drops as the issue tracker for the Matt Pocock skills

[`issue-tracker.md`](issue-tracker.md) in this directory is a ready-made tracker
config for the [`mattpocock/skills`](https://github.com/mattpocock/skills)
engineering suite. Drop it into a repository and `/to-spec`, `/to-tickets`,
`/triage`, `/code-review` and `/wayfinder` will read and write their issues in
drops instead of GitHub, GitLab, or markdown files under `.scratch/`.

The skills themselves need no editing. They resolve the tracker at run time
through one committed markdown file, which is why "other" is a first-class
answer in `/setup-matt-pocock-skills` and why this file is all it takes.

## Install

From the root of the repository whose issues should live in drops:

```sh
mkdir -p docs/agents
curl -fsSL https://raw.githubusercontent.com/tedkulp/drops/main/docs/matt-pocock-skills/issue-tracker.md \
  -o docs/agents/issue-tracker.md
```

`docs/agents/issue-tracker.md` is the path to use. `/wayfinder` finds the file
through the pointer block below and would follow it anywhere, but
`/code-review` names that path literally, so anywhere else costs you an axis of
the review.

That command overwrites whatever tracker config the repo already had. If
`/setup-matt-pocock-skills` has already run there, the file it wrote is the one
being replaced — which is the point, but check it in as its own commit so the
switch is visible.

Then add the pointer to whichever of `CLAUDE.md` or `AGENTS.md` the repo already
has — edit the existing `## Agent skills` block rather than appending a second
one:

```markdown
## Agent skills

### Issue tracker

Issues live in `drops`, scoped to this checkout by its directory. See
`docs/agents/issue-tracker.md`; its **Wayfinding operations** section defines
how to work a map.
```

`drops` has to be on `PATH` for any of it to work — see the
[install section](../../README.md#install) of the main README. Nothing else is
needed: the store is created on first use and the project is resolved from the
working directory, so there is no per-repo setup inside drops itself.

## What this does not cover

`/setup-matt-pocock-skills` writes three files, and this is one of them. The
other two — `docs/agents/domain.md` and, when `/triage` is installed,
`docs/agents/triage-labels.md` — say nothing about the tracker and are worth
getting from the skill. Run it first, answer **other** for the tracker with a
sentence about drops, then overwrite its `issue-tracker.md` with the curl above.

The file makes no claim about your repository — a repo can have a GitHub or
GitLab remote and still track its work in drops, and the config says so — so it
installs unedited. One detail to read past: the **Known rough edges** section
cites row counts from one store while explaining a real trap (`list --parent`
misses migrated children). The counts are an illustration; the trap is
everywhere.

## Why this is not `docs/agents/issue-tracker.md` in this repo

It used to be. This repository builds drops but tracks its own work in
[GitHub Issues](https://github.com/tedkulp/drops/issues), so
`docs/agents/issue-tracker.md` here describes `gh` and the drops template needed
somewhere else to live. It is a template kept for other repositories, and it is
verified against the CLI contract rather than against this repo's own workflow.
