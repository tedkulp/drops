package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// verbNames reads a modal's rows back as the verbs they stand for, which is
// what the applicability table is asserted through: a test can see WHICH verbs
// an issue offers without running one.
func verbNames(verbs []actionVerb) []string {
	names := map[actionVerb]string{
		actClose: "close", actReopen: "reopen", actComment: "comment",
		actPriority: "priority", actClaim: "claim", actRelease: "release",
	}
	out := make([]string, 0, len(verbs))
	for _, verb := range verbs {
		out = append(out, names[verb])
	}
	return out
}

// openActionsOn puts the cursor's issue in the right pane and opens `x`.
func openActionsOn(t *testing.T, pane *Model) {
	t.Helper()
	press(t, pane, "x")
	if pane.modal == nil {
		t.Fatalf("x opened no modal; the notice line says %q", pane.notice)
	}
}

// chooseRow walks the open modal to one row by its text and takes it. The
// returned command is the editor's, when the verb is one that types.
func chooseRow(t *testing.T, pane *Model, want string) tea.Cmd {
	t.Helper()
	for index, row := range pane.modal.rows {
		if strings.HasPrefix(row.Text, want) {
			pane.modal.move(index - pane.modal.selected())
			return pane.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	}
	t.Fatalf("no row starting %q in %v", want, pane.modal.rows)
	return nil
}

func TestTheActionModalOffersOnlyTheVerbsThatApply(t *testing.T) {
	// qy3de.7 §8's table, one case per branch rather than one for the lot.
	// Every row here is a state a followed issue can actually be in, which is
	// why the modal has to decide at all: the left pane only ever shows live
	// work, and `f` reaches everything else.
	claimed := "someone else"
	for _, want := range []struct {
		name  string
		issue model.Issue
		verbs []string
	}{
		{
			"open and unclaimed",
			model.Issue{Status: model.StatusOpen},
			[]string{"close", "comment", "priority", "claim"},
		},
		{
			"in progress is live work too",
			model.Issue{Status: model.StatusInProgress},
			[]string{"close", "comment", "priority", "claim"},
		},
		{
			"open and claimed offers release, never claim",
			model.Issue{Status: model.StatusOpen, Assignee: &claimed},
			[]string{"close", "comment", "priority", "release"},
		},
		{
			"closed offers reopen instead of close",
			model.Issue{Status: model.StatusClosed},
			[]string{"reopen", "comment", "priority"},
		},
		{
			// The claim on a closed issue is the durable record of who
			// resolved it: 38 of the live store's 39 assignees sit on closed
			// issues, so release is withheld rather than offered.
			"closed and claimed offers neither claim nor release",
			model.Issue{Status: model.StatusClosed, Assignee: &claimed},
			[]string{"reopen", "comment", "priority"},
		},
		{
			"tombstoned offers nothing at all",
			model.Issue{Status: model.StatusOpen, Tombstone: model.Tombstoned},
			nil,
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			rows, verbs := actionRows(want.issue, "tester")
			if got := verbNames(verbs); !equalStrings(got, want.verbs) {
				t.Fatalf("verbs = %v, want %v", got, want.verbs)
			}
			if len(rows) != len(verbs) {
				t.Fatalf("%d rows against %d verbs; the picker's index would not line up",
					len(rows), len(verbs))
			}
		})
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestATombstonedIssueSaysThereIsNothingToDo(t *testing.T) {
	// Mirrors `f`'s "nothing to follow", and it is reachable: qy3de.6 §9
	// measured a live relation edge pointing at a tombstoned issue, so
	// following lands on one.
	fixture := newFixture(t)
	here := fixture.issue("The issue the pane is on", 1)
	gone := fixture.issue("An issue deleted underneath you", 2)
	fixture.dep(here.ID, gone.ID, model.DepRelated)
	fixture.tombstone(gone.ID)

	pane := fixture.model(80, 24)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != gone.ID {
		t.Fatalf("detail = %s, want the tombstoned target %s", pane.detailID, gone.ID)
	}

	press(t, pane, "x")
	if pane.modal != nil {
		t.Fatalf("x opened a modal on a tombstoned issue: %v", pane.modal.rows)
	}
	if pane.notice != "nothing to do here" {
		t.Fatalf("notice = %q, want %q", pane.notice, "nothing to do here")
	}
}

func TestXTargetsTheRightPanesIssueNotTheLeftCursor(t *testing.T) {
	// The two differ exactly while the follow stack is non-empty, which is
	// the case euv2e.6 could not have described: it said "the right pane's
	// issue" before there was a stack that could make them disagree.
	fixture := newFixture(t)
	parent := fixture.issue("The issue the cursor is on", 1)
	child := fixture.child(parent.ID, "The issue that is followed to", 2)

	pane := fixture.model(80, 24)
	if pane.cursor.id != parent.ID {
		t.Fatalf("cursor = %s, want the P1 issue %s at the head of the queue", pane.cursor.id, parent.ID)
	}
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != child.ID || pane.cursor.id != parent.ID {
		t.Fatalf("after following, detail = %s and cursor = %s; want %s and %s",
			pane.detailID, pane.cursor.id, child.ID, parent.ID)
	}

	openActionsOn(t, pane)
	if pane.target != child.ID {
		t.Fatalf("x targets %s, want the followed issue %s", pane.target, child.ID)
	}
	// Proved through a write rather than the field alone: the priority verb
	// needs no editor, so the store says which issue the modal was aimed at.
	chooseRow(t, pane, "priority")
	pane.modal.move(4 - pane.modal.selected())
	pane.key(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := fixture.reread(child.ID); got.Priority != 4 {
		t.Fatalf("followed issue %s is P%d, want P4: the write missed its target", child.ID, got.Priority)
	}
	if got := fixture.reread(parent.ID); got.Priority != 1 {
		t.Fatalf("cursor's issue %s is P%d, want P1 untouched", parent.ID, got.Priority)
	}
}

// editorLeaves drives the round trip's second half: the file the editor left
// behind, and the code it exited with. It is where §11's three refusals are
// asserted, because the model's own seam for an editor is the message
// tea.ExecProcess's callback sends back.
func editorLeaves(t *testing.T, pane *Model, kind editKind, id model.ID, body string, exit error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drops-edit.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write editor file: %v", err)
	}
	updated, _ := pane.Update(editorDoneMsg{kind: kind, id: id, path: path, err: exit})
	if updated != tea.Model(pane) {
		t.Fatal("Update returned a different model")
	}
}

func TestAnEmptyEditorFileClosesNothing(t *testing.T) {
	// A close reason is the one place the navigator is deliberately stricter
	// than the CLI, where `--reason` is optional and `drops close <id>`
	// legally stores NULL. It has happened twice in 838 closings; AGENTS.md's
	// "What a ticket records" is why the third case is not offered here.
	fixture := newFixture(t)
	issue := fixture.issue("An issue whose close is abandoned", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editClose, issue.ID, "  \n\t\n  ", nil)

	if got := fixture.reread(issue.ID); got.Status != model.StatusOpen {
		t.Fatalf("status = %s, want it still open: a whitespace-only file closes nothing", got.Status)
	}
	if pane.message != "" {
		t.Fatalf("message = %q, want silence: nothing happened, so nothing is reported", pane.message)
	}
}

func TestAnEmptyEditorFileWritesNoComment(t *testing.T) {
	// This one is free in core — AddComment refuses an empty body as
	// ErrInvalid regardless — which is exactly why it is asserted separately:
	// the clause is that nothing is written AND nothing is said, and letting
	// core refuse it would put an error on the message line.
	fixture := newFixture(t)
	issue := fixture.issue("An issue whose comment is abandoned", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editComment, issue.ID, "\n\n", nil)

	if got := fixture.comments(issue.ID); len(got) != 0 {
		t.Fatalf("thread holds %d comments, want none", len(got))
	}
	if pane.message != "" {
		t.Fatalf("message = %q, want silence rather than core's ErrInvalid", pane.message)
	}
}

func TestANonZeroEditorExitWritesNothing(t *testing.T) {
	// `:cq` is a deliberate cancel, so a file full of text is discarded when
	// the editor says it failed — and unlike the empty file, this one is
	// reported, because something did go wrong.
	fixture := newFixture(t)
	issue := fixture.issue("An issue whose close is cancelled with :cq", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editClose, issue.ID, "a reason typed and then abandoned", errors.New("exit status 1"))

	if got := fixture.reread(issue.ID); got.Status != model.StatusOpen {
		t.Fatalf("status = %s, want it still open: a non-zero exit writes nothing", got.Status)
	}
	if !strings.Contains(pane.message, "exit status 1") {
		t.Fatalf("message = %q, want the editor's failure on it", pane.message)
	}
}

func TestClosingThroughTheEditorStoresTheTrimmedReason(t *testing.T) {
	fixture := newFixture(t)
	issue := fixture.issue("An issue that gets closed", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editClose, issue.ID, "## Done\n\nbecause it is.\n", nil)

	got := fixture.reread(issue.ID)
	if got.Status != model.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	// No `#` stripping: six close reasons in the corpus start a line with
	// one, and a stripper would eat them.
	if got.CloseReason == nil || *got.CloseReason != "## Done\n\nbecause it is." {
		t.Fatalf("close reason = %v, want the file's text trimmed and otherwise untouched", got.CloseReason)
	}
}

func TestCommentingThroughTheEditorIsAttributedToTheResolvedAuthor(t *testing.T) {
	// The TUI is the one surface where the default author is always right:
	// an agent cannot drive an alt-screen program, so the writer is a human
	// at a keyboard. 58 of the corpus's 181 comments have a non-human author,
	// which is the false record this closes off.
	fixture := newFixture(t)
	issue := fixture.issue("An issue that gets a comment", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editComment, issue.ID, "a remark\nover two lines\n", nil)

	got := fixture.comments(issue.ID)
	if len(got) != 1 {
		t.Fatalf("thread holds %d comments, want 1", len(got))
	}
	if got[0].Author != "tester" || got[0].Body != "a remark\nover two lines" {
		t.Fatalf("comment = %+v, want it by tester carrying both lines", got[0])
	}
}

func TestReopenNamesTheLengthOfTheCloseReasonItWillDiscard(t *testing.T) {
	// The prompt is not an "are you sure": SetIssueStatus sets CloseReason =
	// nil on the way to StatusOpen and close_reason is a single column, so
	// reopening DESTROYS it. 81 of the corpus's run past 2,000 characters.
	// What the reader cannot otherwise see is how much is about to go, so
	// that is what the line states.
	fixture := newFixture(t)
	issue := fixture.issue("An issue with a long close reason", 1)
	reason := strings.Repeat("x", 2317)
	fixture.close(issue.ID, reason)

	pane := fixture.model(80, 24)
	press(t, pane, "C")
	openActionsOn(t, pane)
	chooseRow(t, pane, "reopen")

	if pane.ask == nil {
		t.Fatal("reopen wrote straight through; it is the one verb that asks first")
	}
	if !strings.Contains(pane.ask.prompt, "2317-character") {
		t.Fatalf("prompt = %q, want the actual length of the reason it discards", pane.ask.prompt)
	}
	// The prompt is on the frame, not buried in a field, and it costs a pane
	// row exactly while it is up.
	if !strings.Contains(pane.frame(), "2317-character") {
		t.Fatal("the prompt is not on the frame")
	}
}

func TestAnsweringTheReopenPromptWithAnythingButYWritesNothing(t *testing.T) {
	fixture := newFixture(t)
	issue := fixture.issue("An issue that stays closed", 1)
	fixture.close(issue.ID, "the reason it was closed for")

	pane := fixture.model(80, 24)
	press(t, pane, "C")
	openActionsOn(t, pane)
	chooseRow(t, pane, "reopen")
	press(t, pane, "n")

	if pane.ask != nil {
		t.Fatal("the prompt is still up; n dismisses it")
	}
	got := fixture.reread(issue.ID)
	if got.Status != model.StatusClosed {
		t.Fatalf("status = %s, want it still closed", got.Status)
	}
	if got.CloseReason == nil || *got.CloseReason != "the reason it was closed for" {
		t.Fatalf("close reason = %v, want it intact", got.CloseReason)
	}
}

func TestAnsweringTheReopenPromptWithYReopensAndDiscardsTheReason(t *testing.T) {
	fixture := newFixture(t)
	issue := fixture.issue("An issue that gets reopened", 1)
	fixture.close(issue.ID, "the reason it was closed for")

	pane := fixture.model(80, 24)
	press(t, pane, "C")
	openActionsOn(t, pane)
	chooseRow(t, pane, "reopen")
	press(t, pane, "y")

	got := fixture.reread(issue.ID)
	if got.Status != model.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
	if got.CloseReason != nil {
		t.Fatalf("close reason = %v, want it gone: that is what the prompt warned about", *got.CloseReason)
	}
}

func TestACredentialInACommentReachesTheFrame(t *testing.T) {
	// qy3de.7 §5, and it was found rather than inherited: every write in the
	// set runs core's credential scan, whose sink writes to STDERR, which a
	// full-screen program makes invisible. Left alone the navigator would
	// scan every comment you type for an AWS key and silently discard the
	// finding.
	fixture := newFixture(t)
	issue := fixture.issue("An issue that gets a careless comment", 1)
	pane := fixture.model(80, 24)

	editorLeaves(t, pane, editComment, issue.ID, "the key is AKIAIOSFODNN7EXAMPLE, do not lose it", nil)

	if !strings.Contains(pane.message, "AWS access key id") {
		t.Fatalf("message = %q, want the credential family on it", pane.message)
	}
	if !strings.Contains(pane.frame(), "AWS access key id") {
		t.Fatal("the warning is not on the frame; it would have gone to stderr under an alt screen")
	}
	// The comment still commits: a warning is advisory and never changes
	// whether a write happens.
	if got := fixture.comments(issue.ID); len(got) != 1 {
		t.Fatalf("thread holds %d comments, want the write to have committed", len(got))
	}
}

func TestAFailedWriteLeavesTheStoreUnchangedAndSurfacesOnTheMessageLine(t *testing.T) {
	// The realistic failure is a claim landing between the render and the
	// `enter`: the modal offered `claim` because the issue was unassigned
	// when the page was drawn, and another session took it in between.
	fixture := newFixture(t)
	issue := fixture.issue("An issue two sessions both want", 1)
	pane := fixture.model(80, 24)
	openActionsOn(t, pane)

	fixture.claim(issue.ID, "the other session")
	chooseRow(t, pane, "claim")

	if pane.message == "" {
		t.Fatal("a failed write said nothing; ErrConflict has nowhere else to go under an alt screen")
	}
	if !strings.Contains(pane.message, "already claimed") {
		t.Fatalf("message = %q, want core's conflict on it", pane.message)
	}
	if got := assigneeOf(fixture.reread(issue.ID)); got != "the other session" {
		t.Fatalf("assignee = %q, want the other session's claim untouched", got)
	}
}

func TestAPriorityWriteCarriesTheCursorToTheRowsNewPosition(t *testing.T) {
	// `ORDER BY issues.priority` is the store's first key, so changing one
	// re-orders the list under the reader. The cursor tracks an ID, so it
	// rides the row to its new place rather than holding an index and
	// selecting someone else — precisely the defect qy3de.4 wrote a control
	// for, and the write set is the first thing on this map that triggers it.
	fixture := newFixture(t)
	// The CLI's order is priority ascending, then NEWEST first, so the P2
	// written first is the one at the bottom of the queue.
	fixture.issue("The P0 issue at the head of the queue", 0)
	moved := fixture.issue("The P2 issue that becomes P1", 2)
	fixture.issue("A newer P2 issue", 2)

	pane := fixture.model(80, 24)
	press(t, pane, "G")
	if pane.cursor.id != moved.ID || pane.cursor.position != 2 {
		t.Fatalf("cursor = %s at %d, want %s at 2", pane.cursor.id, pane.cursor.position, moved.ID)
	}

	openActionsOn(t, pane)
	chooseRow(t, pane, "priority")
	pane.modal.move(1 - pane.modal.selected())
	pane.key(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := fixture.reread(moved.ID); got.Priority != 1 {
		t.Fatalf("priority = %d, want 1", got.Priority)
	}
	if pane.cursor.id != moved.ID {
		t.Fatalf("cursor = %s, want it still on %s", pane.cursor.id, moved.ID)
	}
	if pane.cursor.position != 1 {
		t.Fatalf("cursor position = %d, want 1: the row moved and the cursor rode it",
			pane.cursor.position)
	}
	if pane.detailID != moved.ID {
		t.Fatalf("detail = %s, want it following the cursor to %s", pane.detailID, moved.ID)
	}
}

func TestClosingTheCursorsIssueWalksTheListDownwards(t *testing.T) {
	// The other half of qy3de.7 §9, and a payoff of the same cursor rule
	// rather than a surprise: with `C` off the closed row leaves the set, the
	// cursor holds the position index it occupied, and close-close-close
	// walks the list.
	fixture := newFixture(t)
	first := fixture.issue("The first issue", 1)
	second := fixture.issue("The second issue", 2)

	pane := fixture.model(80, 24)
	if pane.cursor.id != first.ID {
		t.Fatalf("cursor = %s, want %s", pane.cursor.id, first.ID)
	}
	openActionsOn(t, pane)
	chooseRow(t, pane, "close")
	editorLeaves(t, pane, editClose, first.ID, "done", nil)

	if len(pane.rows) != 1 {
		t.Fatalf("%d rows, want the closed one gone with C off", len(pane.rows))
	}
	if pane.cursor.id != second.ID || pane.cursor.position != 0 {
		t.Fatalf("cursor = %s at %d, want %s at 0", pane.cursor.id, pane.cursor.position, second.ID)
	}
	if pane.detailID != second.ID {
		t.Fatalf("detail = %s, want it on the row the cursor landed on", pane.detailID)
	}
}

func TestAWriteToAFollowedIssueLeavesTheLeftPaneAlone(t *testing.T) {
	fixture := newFixture(t)
	parent := fixture.issue("The issue the cursor is on", 1)
	child := fixture.child(parent.ID, "The issue that is written to", 2)

	pane := fixture.model(80, 24)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)

	openActionsOn(t, pane)
	chooseRow(t, pane, "claim")

	if got := assigneeOf(fixture.reread(child.ID)); got != "tester" {
		t.Fatalf("assignee = %q, want tester", got)
	}
	if pane.cursor.id != parent.ID {
		t.Fatalf("cursor = %s, want it still on %s: following never moved it", pane.cursor.id, parent.ID)
	}
	if pane.detailID != child.ID || len(pane.stack) != 1 {
		t.Fatalf("detail = %s at depth %d, want %s at depth 1: a write does not pop the trail",
			pane.detailID, len(pane.stack), child.ID)
	}
}

func TestTheMessageLineCostsAPaneRowOnlyWhileItSaysSomething(t *testing.T) {
	// It comes out of the panes rather than the footer, so qy3de.4's
	// disclosure line and qy3de.5's scroll indicator are undisturbed by a
	// write failure.
	fixture := newFixture(t)
	issue := fixture.issue("An issue two sessions both want", 1)
	pane := fixture.model(80, 24)
	quiet := pane.frame()
	if pane.messageLines() != 0 {
		t.Fatal("a pane with nothing to say is already spending a row on it")
	}

	openActionsOn(t, pane)
	fixture.claim(issue.ID, "the other session")
	chooseRow(t, pane, "claim")
	loud := pane.frame()

	if pane.messageLines() != 1 {
		t.Fatalf("message = %q but the line costs %d rows", pane.message, pane.messageLines())
	}
	if strings.Count(quiet, "\n") != strings.Count(loud, "\n") {
		t.Fatalf("frame heights differ: %d lines quiet, %d loud",
			strings.Count(quiet, "\n")+1, strings.Count(loud, "\n")+1)
	}
	// The footer is where the row came from NOT being taken: scope, count and
	// every active mode read the same under a message as without one.
	if lastLine(loud) != lastLine(quiet) {
		t.Fatalf("footer changed under a message:\n quiet %q\n loud  %q", lastLine(quiet), lastLine(loud))
	}
	// The panes are what pay for it, and the row comes back when it clears.
	// `k` on the first row is a clamped no-op, so the only thing it changes
	// is the message.
	if pane.geo().rows != measure(80, 24, false).rows-1 {
		t.Fatalf("panes hold %d rows under a message, want one fewer than %d",
			pane.geo().rows, measure(80, 24, false).rows)
	}
	press(t, pane, "k")
	if pane.message != "" || pane.messageLines() != 0 {
		t.Fatalf("message = %q, want the next keypress to have cleared it", pane.message)
	}
	if pane.geo().rows != measure(80, 24, false).rows {
		t.Fatal("the panes did not get their row back")
	}
}

func lastLine(frame string) string {
	lines := strings.Split(frame, "\n")
	return lines[len(lines)-1]
}
