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
// R. It also POLLS, every pollInterval, and the poll's job is noticing rather
// than reloading: it reads store_state.write_seq — the counter bumped inside
// the same transaction as every replicated write — and puts `stale · R` on the
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
// and R do: the stack stands, the filter stands, the cursor rides its tracked
// id, and the right pane is REDRAWN rather than reopened — same page, same
// scroll position.
//
// That last part is decided on WHAT IS ON SCREEN and not on which motion the
// caller meant. showDetail holds the reader's offsets while the id it is handed
// is the one already there and the width it renders into is the width that page
// was rendered at, and starts at the top otherwise: a different issue is a page
// nobody scrolled, and a different width has reflowed the one they did. So a
// refresh whose cursor restore walked onto another issue opens it at line 0
// (tc2x5, which is what a `keep` flag believed instead), and `enter`-zoom and a
// resize start at the top as they always did. Unchanged is tested on the
// rendered text rather than the issue's revision, because a comment is its own
// record and would not move the revision of the page it appears on.
//
// There is deliberately no suppression around any of this. A refresh cannot be
// reached from inside a modal or the / prompt — both capture every key before
// the keymap sees one — so a guard would be a branch whose control could never
// go red, which is precisely the defect AGENTS.md names. The reachability is
// asserted by a test instead.
//
// `r` is NEITHER motion: g7b23's ready-only toggle reads nothing at all. It is
// a narrowing, beside `/` — the predicate is blockedBy == 0 over rows the pane
// already holds — so it goes through applyFilter, drops the follow stack the
// way a filter keystroke does, and survives C and a because every reload
// re-derives the visible set through that one function. It is deliberately not
// core.Ready: re-asking the store would throw away what the pane's own modes
// had put on screen — the closed rows C added, and a deferred issue, which
// core.Ready omits. Keeping the predicate local is what leaves the row set as
// list's contract under it.
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
// # Getting an id out
//
// `y` (bvjgn) sends the RIGHT PANE'S id to the system clipboard over OSC 52,
// through tea.SetClipboard, which is the whole implementation: no second
// process, and this repository ships one static binary. It names the same
// issue `x` does, so a follow trail cannot make two keys mean two things.
//
// OSC 52 is unacknowledged — the terminal takes the sequence or ignores it and
// the program cannot tell which — so the footer says what was SENT rather than
// what was pasted. It is m.notice and not m.message for the reason qy3de.14
// §Q5 gave the stale marker, run the other way: a message costs a pane row,
// and the acknowledgement of a keystroke that wrote nothing must die on the
// next keypress rather than survive `j`.
package tui
