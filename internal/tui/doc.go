// Package tui is the two-pane issue navigator behind `drops tui`.
//
// The left pane lists every live issue in one scope, in the CLI's order; the
// right pane is one issue in full. The whole point is that the right pane can
// be retargeted without the left cursor moving, so following a relation costs
// nothing and loses nothing.
//
// # Where it sits
//
// tui imports core, model, render and view. internal/cli builds the command
// tree and therefore imports tui, never the reverse (qy3de.3), which is why
// everything discovered — the *core.Core, the resolved model.Project, the
// comment author, the context — arrives as a parameter rather than being
// looked up here. Terminal size is the one exception, and only because
// bubbletea delivers it as a message.
//
// There is deliberately no Store or Reader interface. AGENTS.md's seam rule
// puts a discovered FACT behind a parameter and leaves a real subsystem real:
// every test in this package runs against real temp SQLite, exactly like the
// rest of the repository.
//
// # What renders what
//
// The detail pane renders through render.Console into a bytes.Buffer,
// unchanged — TTY false, no pager, the measured pane width — and
// bubbles/viewport clips the overflow. render.Console.Page truncates nothing,
// on purpose, so 43% of open issues emit a line wider than a 37-column pane
// (measured for qy3de.3). Those bytes are off-screen and reachable: `h` `l`
// scroll to them and `w` soft-wraps instead.
//
// The left pane's row geometry lives HERE rather than in render, because the
// pane deliberately truncates an id and render's doc forbids exactly that. The
// truncation primitive is still render's (render.Ellipsis, render.Cols): the
// policy differs, the rule does not.
//
// # Refreshing
//
// The pane reads the store when it starts, on C and a, after a write, and on
// r. It also POLLS, every pollInterval, and the poll's job is noticing rather
// than reloading: it reads store_state.write_seq — the counter bumped inside
// the same transaction as every replicated write — and puts `stale` on the
// footer when it has moved. It does not re-read, because this store's common
// writer is an agent and a surface that reorders itself under a reader
// mid-thought is the thing this package exists to avoid (qy3de.14).
//
// That split is affordable because the two costs are three orders of magnitude
// apart, measured against the 1025-issue corpus: the token is one primary-key
// row at 4.2µs, where a whole refresh is 5.5ms scoped and 10.0ms wide, nearly
// all of it core.OpenBlockers at 5.24ms.
//
// reload and refresh are different motions and the difference is navigational,
// not about reading. reload is what a scope change does: it clears the follow
// stack, because the row set underneath it changed. refresh is what a write
// and r do: the stack stands, the filter stands, the cursor rides its tracked
// id, and the right pane is REDRAWN rather than reopened — same page, same
// scroll position. drawDetail's keep parameter is that distinction, and
// unchanged is tested on the rendered text rather than the issue's revision,
// because a comment is its own record and would not move the revision of the
// page it appears on.
//
// There is deliberately no suppression around any of this. A refresh cannot be
// reached from inside a modal or the / prompt — both capture every key before
// the keymap sees one — so a guard would be a branch whose control could never
// go red, which is precisely the defect AGENTS.md names. The reachability is
// asserted by a test instead.
//
// # Following
//
// `f` opens a picker over the right pane's relations and `enter` retargets the
// pane onto one, pushing onto a stack of ids that `backspace` walks back down;
// moving the left cursor clears it. That there is a MODAL at all is forced
// rather than chosen: the detail pane is an opaque render.Console buffer, so
// the ref lines already on screen are not addressable, and re-listing them
// from the core.IssueView is the only way to interact with one without
// breaking that opacity (qy3de.6 §3).
//
// The picker's boundary is the full core graph, wider than the page beside it:
// a page renders `blocks` alone, which is an omission on show's side rather
// than a designed reading contract (drops://hxedy). Until that lands, this is
// the only surface in drops that can read a `discovered-from` edge back.
package tui
