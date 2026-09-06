# drops

A cross-project issue tracker for AI coding agents. Go, pure-Go SQLite, and
Cobra; one database for every repository, with the project resolved from the
working directory.

**Read [AGENTS.md](AGENTS.md) before working here.** It carries the gate, the
scratch-store rule, the test discipline every ticket is held to, and the tools in
this environment that report success while failing.

Issues live in **GitHub Issues** on
[`tedkulp/drops`](https://github.com/tedkulp/drops), driven by the `gh` CLI —
not in the `drops` store this repo builds. `docs/agents/issue-tracker.md` is the
contract for working them; the open queue there is the work queue, and
checked-in notes are not a parallel plan.

The `dw32p` rewrite map that carried this repo to 0.2.0 is closed. Its 79 issues
stay in `~/.drops/drops.db`, which is why `dw32p.N` ids appear in `CHANGELOG.md`
and `docs/research/`.
