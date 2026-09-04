package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// modalKind is what `enter` does with the open picker. The component is one
// thing with three call sites (qy3de.7 §8), so the model needs to know which
// of them it is looking at rather than which field holds it.
type modalKind int

const (
	modalFollow modalKind = iota
	modalActions
	modalPriority
)

// actionVerb is one row of the Actions modal.
//
// It is an enum rather than a closure per row because the rows are also what
// the applicability table in qy3de.7 §8 is asserted against: a test can read
// back WHICH verbs an issue offers without running any of them.
type actionVerb int

const (
	actClose actionVerb = iota
	actReopen
	actComment
	actPriority
	actClaim
	actRelease
)

// priorities is the second picker's rows: qy3de.7 §8 kept priority a step of
// its own rather than five flattened rows, so the Actions modal stays a list
// of verbs. The corpus is the other half of that argument — P0 has 1 live
// issue and P4 has 4, against P2's 76 — so five rows would have been spent on
// a range almost nothing uses.
const priorities = 5

// question is the y/n line reopen puts up (qy3de.7 §7).
//
// It exists for exactly one branch: SetIssueStatus sets CloseReason = nil on
// the way to StatusOpen, and close_reason is a single column, so a one-key
// reopen from the navigator DESTROYS the reason. 81 of the corpus's are past
// 2,000 characters. The prompt is not an "are you sure": it states the size of
// what is about to be lost, which is the thing a reader cannot otherwise see.
type question struct {
	prompt  string
	confirm func()
}

// actionRows is qy3de.7 §8's applicability table: one row per verb that
// applies to this issue's state, in the order the modal lists them.
//
// A TOMBSTONED issue gets no rows at all. It is unreachable from the left pane
// and reachable by following, and every core write refuses it with
// ErrConflict, so a modal offering six verbs that all fail is worse than one
// that says there is nothing to do.
//
// claim and release are gated on OPEN work, which is policy rather than
// anything core enforces: ClaimIssue permits a closed issue, refusing only a
// tombstone and an existing claim. The live store is the reason — 38 of its 39
// assignees sit on closed issues, so a claim is the durable record of who
// resolved a ticket, and `release` on a closed one would erase that in a
// keypress with nothing asking first.
func actionRows(issue model.Issue, author string) ([]pickerRow, []actionVerb) {
	if issue.Tombstone == model.Tombstoned {
		return nil, nil
	}
	live := issue.Status == model.StatusOpen || issue.Status == model.StatusInProgress
	held := assigneeOf(issue)

	rows, verbs := []pickerRow{}, []actionVerb{}
	add := func(verb actionVerb, text string) {
		rows = append(rows, pickerRow{Text: text})
		verbs = append(verbs, verb)
	}
	if live {
		add(actClose, "close")
	}
	if !live {
		add(actReopen, "reopen")
	}
	add(actComment, "comment")
	add(actPriority, fmt.Sprintf("priority · now P%d", issue.Priority))
	if live && held == "" {
		// The name is on the row because claim types nothing: the modal is
		// about to write a name the reader never entered.
		add(actClaim, "claim as "+author)
	}
	if live && held != "" {
		add(actRelease, "release · held by "+held)
	}
	return rows, verbs
}

// assigneeOf reads a claim, treating the historical empty string as unclaimed
// the way the tracker doc's frontier query does.
func assigneeOf(issue model.Issue) string {
	if issue.Assignee == nil {
		return ""
	}
	return *issue.Assignee
}

// openActions is `x`: the write set as one picker over WHATEVER THE RIGHT PANE
// IS SHOWING — the followed target when the stack is non-empty, the cursor's
// issue otherwise.
//
// That is restated rather than inherited from euv2e.6, which said "the right
// pane's issue" before there was a follow stack that could make the two
// differ. The issue is read from the view the page beside it was drawn from,
// so opening the modal costs no store read at all.
func (m *Model) openActions() {
	if m.detailID == "" {
		return
	}
	rows, verbs := actionRows(m.detailView.Issue, m.author)
	if len(verbs) == 0 {
		m.notice = "nothing to do here"
		return
	}
	m.target = m.detailID
	opened := newPicker("Actions", rows)
	m.modal, m.kind, m.verbs = &opened, modalActions, verbs
	m.modal.scroll(m.detailRows(m.geo()))
}

// runVerb is `enter` on one of the six rows.
//
// close and comment return a command rather than writing, because both go
// through $EDITOR: the corpus settles that argument and it is not close — 178
// of 181 comment bodies run past 80 characters, 110 are multi-line, and 81
// close reasons run past 2,000. Text of that shape is not entered into a
// textarea in a 38-column pane, so bubbles/textarea is not imported at all.
func (m *Model) runVerb(verb actionVerb, id model.ID) tea.Cmd {
	switch verb {
	case actClose:
		m.closeModal()
		return m.edit(editClose, id)
	case actComment:
		m.closeModal()
		return m.edit(editComment, id)
	case actPriority:
		// The second picker opens over the same target, so it is NOT closed
		// here: only the Actions rows are replaced. The cursor starts on the
		// issue's current priority, which is the same disclosure the row it
		// was chosen from carries.
		rows := make([]pickerRow, 0, priorities)
		for level := range priorities {
			rows = append(rows, pickerRow{Text: fmt.Sprintf("P%d", level)})
		}
		opened := newPicker("Priority", rows)
		opened.move(m.detailView.Issue.Priority)
		m.modal, m.kind, m.verbs = &opened, modalPriority, nil
		m.modal.scroll(m.detailRows(m.geo()))
	case actReopen:
		m.closeModal()
		m.askToReopen(id)
	case actClaim:
		m.closeModal()
		m.write(func() error {
			_, err := m.source.core.ClaimIssue(m.ctx, id, m.author)
			return err
		})
	case actRelease:
		m.closeModal()
		m.write(func() error {
			_, err := m.source.core.ReleaseIssue(m.ctx, id)
			return err
		})
	}
	return nil
}

// askToReopen puts up the one prompt in the write set, naming the length of
// the close reason reopening will discard.
func (m *Model) askToReopen(id model.ID) {
	reason := ""
	if m.detailView.Issue.CloseReason != nil {
		reason = *m.detailView.Issue.CloseReason
	}
	m.ask = &question{
		prompt: fmt.Sprintf("reopen %s? this discards its %d-character close reason  [y/N]",
			id, len(reason)),
		confirm: func() {
			m.write(func() error {
				_, err := m.source.core.SetIssueStatus(m.ctx, id, model.StatusOpen, "")
				return err
			})
		},
	}
}

// setPriority is `enter` on one of the Priority rows. level is the picker's
// index, which IS the priority: the rows are P0 through P4 in order.
func (m *Model) setPriority(id model.ID, level int) {
	m.write(func() error {
		_, err := m.source.core.EditIssue(m.ctx, id, core.IssueEdit{Priority: &level})
		return err
	})
}

// write runs one store write and turns everything it produces into the message
// line: the failure, or the credential warnings the alt screen would otherwise
// swallow, or neither.
//
// ErrConflict and ErrNotFound are the realistic ones — tombstoned or claimed
// or deleted underneath you between the render and the `enter` — and neither
// is a fault of this session, so they are a line rather than a modal. A failed
// write leaves the store alone: core's writes are one transaction each, so
// there is nothing here to undo.
func (m *Model) write(do func() error) {
	err := do()
	if err != nil {
		m.message = "error: " + err.Error()
		return
	}
	m.drainWarnings()
	m.fail(m.refresh())
}

// drainWarnings empties the channel cli redirected core's warning sink onto.
//
// It is drained HERE, synchronously after the write, rather than received as a
// tea.Cmd: core.emitWarnings runs inside the same call, on this goroutine,
// after the transaction commits, so by the time write returns every warning
// the write produced is already in the buffer. A nil channel never fires, so
// a pane built without one falls straight through the default.
func (m *Model) drainWarnings() {
	for {
		select {
		case warning := <-m.warnings:
			m.note(warningLine(warning))
		default:
			return
		}
	}
}

// warningLine is one credential finding as the message line says it. It names
// the family and the field and never the matched text, which is the whole
// point of a warning about a secret.
func warningLine(warning core.Warning) string {
	return fmt.Sprintf("%s matched in %s %s (%s)",
		warning.Family, warning.Entity.Kind, warning.Entity.Key, warning.Field)
}

// note appends to the message line, so a write that produces two findings
// reports both in the one slot §10 gives it.
func (m *Model) note(line string) {
	if m.message == "" {
		m.message = line
		return
	}
	m.message += " · " + line
}
