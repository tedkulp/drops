package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// hscrollStep is how far one horizontal keypress moves the detail pane.
const hscrollStep = 8

// The pane's colours. Colour, --no-color and NO_COLOR are still fog on the map
// (qy3de.3 and qy3de.5 both left them open), so these are the prototype's
// legible defaults and not a decision: reverse video for the cursor row, one
// colour for the borders, one for the footer.
var (
	borderColor = lipgloss.Color("62")
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	headStyle   = lipgloss.NewStyle().Bold(true)
)

// Model is the navigator: the row set and its cursor on the left, one issue in
// full on the right, and the disclosure line under both.
type Model struct {
	source *source
	ctx    context.Context

	// author is who a comment written from here is attributed to. Resolved
	// once at command construction and passed in, because cli imports tui
	// and tui cannot reach back for it. The write set that spends it is
	// qy3de.13; it is carried from here so the constructor does not change
	// shape when that lands.
	author string

	all    []row // the loaded set, in the CLI's order, never re-sorted
	rows   []row // what the filter leaves
	scope  scope
	filter string
	typing bool // the `/` prompt is open
	cursor cursor

	detail     viewport.Model
	detailID   model.ID
	detailView core.IssueView
	pageText   string

	// follow is qy3de.6's relation picker: non-nil exactly while it is open,
	// and while it is open it captures every key. choices is aligned with
	// its rows by construction, built in the same pass.
	//
	// stack is the trail BEHIND the right pane — the ids you followed out
	// of, deepest last — so its length is the depth and its top is where
	// `backspace` goes. The right pane's current id is not on it.
	follow  *picker
	choices []followChoice
	stack   []model.ID

	// notice is one transient line for something the pane has to say that is
	// not an error and has nowhere else to go: `f` on an issue with no
	// relations. It lives until the next keypress.
	notice string

	width, height int
	zoomed        bool
	help          bool
	err           error
}

// New builds the navigator over one core and one already-resolved project.
//
// Every discovered fact is a parameter, per AGENTS.md's seam rule: the core,
// the project cli resolved, and the comment author cli worked out. Terminal
// size is the one thing not passed, because bubbletea delivers it as a
// message.
func New(issues *core.Core, project model.Project, author string) *Model {
	return &Model{
		source: &source{core: issues, project: project},
		author: author,
		detail: viewport.New(),
		// A frame is composed before the first tea.WindowSizeMsg arrives;
		// these are the size it is composed at until one does.
		width:  render.DefaultWidth,
		height: 24,
	}
}

// Run reads the row set and runs the program to completion.
//
// The context is threaded to tea.WithContext, so a cancelled context stops the
// program rather than leaving a full-screen process behind, and it is the
// context every read this pane makes is issued under.
func (m *Model) Run(ctx context.Context) error {
	return m.run(ctx)
}

// run is Run with room for the program options a headless test needs.
func (m *Model) run(ctx context.Context, options ...tea.ProgramOption) error {
	m.ctx = ctx
	if err := m.reload(); err != nil {
		return err
	}
	options = append([]tea.ProgramOption{tea.WithContext(ctx)}, options...)
	_, err := tea.NewProgram(m, options...).Run()
	return err
}

// reload re-reads the row set for the current scope and re-points the cursor
// and the detail pane at whatever survived.
func (m *Model) reload() error {
	rows, err := m.source.rows(m.ctx, m.scope)
	if err != nil {
		return err
	}
	m.all = rows
	return m.applyFilter()
}

// applyFilter narrows the loaded set and restores the cursor onto it.
func (m *Model) applyFilter() error {
	m.clearFollow()
	m.rows = matching(m.all, m.filter)
	m.cursor.restore(m.rows)
	return m.showDetail(m.cursor.id)
}

// showDetail retargets the right pane. It does NOT move the left cursor, which
// is the whole navigational claim this map is built on.
func (m *Model) showDetail(id model.ID) error {
	if id == "" {
		m.detailID, m.pageText = "", ""
		m.detail.SetContent("")
		return nil
	}
	geo := measure(m.width, m.height, m.zoomed)
	issue, err := m.source.issue(m.ctx, id)
	if err != nil {
		return err
	}
	text, err := m.source.page(issue, geo.detailWidth)
	if err != nil {
		return err
	}
	m.detailID, m.detailView, m.pageText = id, issue, text
	m.detail.SetContent(text)
	m.detail.SetYOffset(0)
	m.detail.SetXOffset(0)
	return nil
}

// selectRow moves the cursor and retargets the detail pane with it.
func (m *Model) selectRow(to int) error {
	m.clearFollow()
	m.cursor.move(m.rows, to)
	return m.showDetail(m.cursor.id)
}

// clearFollow drops the follow stack and any open picker.
//
// Moving the left cursor does this, and that is what let a key be REMOVED from
// the design rather than overloaded: euv2e.5 gave `esc` a fourth ordered
// meaning to clear the stack, and it saves nothing, because the right pane has
// already returned to the cursor's own issue by the time you could press it.
// `j` — a key you were about to press anyway — is the clear.
func (m *Model) clearFollow() { m.stack, m.follow, m.choices = nil, nil, nil }

// openFollow is `f`: the relation picker over the FULL core graph, always.
//
// euv2e.5's one-relation shortcut is dropped (qy3de.6 §2). It fires on the
// commonest non-zero case — 44 of 174 issues across the corpus carry exactly
// one relation, against 33 with more — so dropping it is not marginal, and it
// costs one keystroke there. What it buys is one meaning for `f`: a
// conditional follow is a load-bearing branch keyed on state the reader cannot
// see without counting the pane's ref lines, and it skips the one moment that
// says WHICH KIND of relation is about to be taken. That matters most for
// `discovered-from`, whose targets are 100% closed.
func (m *Model) openFollow() {
	if m.detailID == "" {
		return
	}
	rows, choices := followRows(m.detailView)
	if len(choices) == 0 {
		m.notice = "nothing to follow"
		return
	}
	// The title stays empty: the group headers already say what the rows
	// are, and the box's border title is the frame's untruncated id, because
	// you are picking a relation OF that issue.
	opened := newPicker("", rows)
	m.follow, m.choices = &opened, choices
	m.follow.scroll(m.detailRows(measure(m.width, m.height, m.zoomed)))
}

// pickFollow is `enter` inside the picker. The choice is looked up by the
// index the picker returns, which is aligned with it by construction.
func (m *Model) pickFollow() {
	chosen := m.choices[m.follow.selected()]
	m.follow, m.choices = nil, nil
	m.fail(m.pushFollow(chosen.id))
}

// pushFollow retargets the right pane onto a relation, remembering where it
// came from. The left cursor does not move: that is the whole navigational
// claim this map is built on.
func (m *Model) pushFollow(id model.ID) error {
	m.stack = append(m.stack, m.detailID)
	return m.showTarget(id)
}

// popFollow is `backspace`. At depth 0 it is a NO-OP — there is nothing
// beneath, and popping an empty stack is the defect this shape produces.
func (m *Model) popFollow() error {
	if len(m.stack) == 0 {
		return nil
	}
	back := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return m.showTarget(back)
}

// showTarget retargets the right pane, falling back DOWN the stack when the
// read fails (qy3de.6 §9).
//
// Following leaves the row set on purpose: 52% of relation targets in the
// corpus are closed, and `C` governs the left pane only. So a target can be
// tombstoned by another replica between the read that listed it and the read
// that opens it. That is a normal outcome in a replicated store rather than a
// fault, so there is no error modal: pop one level and render what is beneath,
// down to the left cursor's own issue, which is always readable.
func (m *Model) showTarget(id model.ID) error {
	for {
		err := m.showDetail(id)
		if err == nil || len(m.stack) == 0 {
			return err
		}
		id = m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// detailRows is the right pane's BODY height: the pane's rows, less the depth
// line while the follow stack is non-empty. The box still holds geo.rows
// either way, so the frame stays square.
func (m *Model) detailRows(geo geometry) int {
	if len(m.stack) == 0 {
		return geo.rows
	}
	return max(geo.rows-1, 1)
}

// depthLine is qy3de.6 §8: `← <previous id> ·<depth>` at the top of the right
// pane, shown only while the stack is non-empty. Naming the origin says what
// `backspace` will do; the count says how many presses gets home.
//
// It is a body line and not the border title, which was the tempting home
// since it costs no row. The title has exactly ZERO headroom, measured: the
// box is 40 columns, the corners and their spaces take 6, and the corpus's
// longest open id is exactly the 34 that leaves. Any marker there truncates
// the id, destroying the guarantee the title exists for — which is the same
// guarantee qy3de.4 broke the never-truncate rule on the strength of.
func (m *Model) depthLine(width int) string {
	if len(m.stack) == 0 {
		return ""
	}
	previous := m.stack[len(m.stack)-1]
	return dimStyle.Render(render.Ellipsis(
		fmt.Sprintf("← %s ·%d", previous, len(m.stack)), width))
}

// Init satisfies tea.Model. Nothing is deferred to a command: the row set is
// already loaded by the time the program starts, so a failed read is an error
// from Run rather than a pane that renders an apology.
func (m *Model) Init() tea.Cmd { return nil }

// Update handles the two messages this pane reads: the terminal's size, and a
// key.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		m.resize()
		// The page was rendered at the old pane width, so it is re-rendered
		// at the new one rather than clipped or stretched.
		m.fail(m.showDetail(m.detailID))
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(message)
	}
	return m, nil
}

// halfPage is how far ^d and ^u move the list cursor: half the rows a pane
// shows. Both panes get the same row budget, so it is one number.
func (m *Model) halfPage() int { return max(measure(m.width, m.height, m.zoomed).rows/2, 1) }

// resize hands the viewport the geometry the frame will use.
func (m *Model) resize() {
	geo := measure(m.width, m.height, m.zoomed)
	m.detail.SetWidth(geo.detailWidth)
	m.detail.SetHeight(m.detailRows(geo))
}

// fail records what a read error does mid-session. There is nowhere to print
// under an alt screen — stderr is invisible there, which is the same reason
// `drops tui` refuses to start rather than exiting 0 with an advisory — so the
// error goes on the footer, which is the pane's disclosure line, and the pane
// keeps the rows it already had rather than blanking.
//
// It takes nil too, so the next read that succeeds clears the message.
func (m *Model) fail(err error) { m.err = err }

// key is the whole keymap.
func (m *Model) key(pressed tea.KeyPressMsg) tea.Cmd {
	key := pressed.String()
	// A notice lives until the next keypress, whatever that key was.
	m.notice = ""

	// The picker captures input while it is open, which is what keeps `esc`
	// unambiguous: cancelling the picker is not a fourth meaning in qy3de.4's
	// ordering, because nothing else can be reached from here.
	if m.follow != nil {
		return m.pickerKey(key)
	}

	// The `/` prompt swallows every printable key, so a filter may contain
	// `q`, `a` and `C` without quitting or changing scope.
	if m.typing {
		switch key {
		case "enter":
			m.typing = false
		case "esc":
			m.typing, m.filter = false, ""
			m.fail(m.applyFilter())
		case "backspace":
			if runes := []rune(m.filter); len(runes) > 0 {
				m.filter = string(runes[:len(runes)-1])
				m.fail(m.applyFilter())
			}
		default:
			// Text is non-empty exactly for a printable keypress, which is
			// how a space reaches the filter: its String() is "space".
			if pressed.Text != "" {
				m.filter += pressed.Text
				m.fail(m.applyFilter())
			}
		}
		return nil
	}

	switch key {
	case "q", "ctrl+c":
		// A full-screen program that swallows ctrl+c is worse than one
		// that does not (qy3de.4).
		return tea.Quit
	case "?":
		m.help = !m.help

	// --- the row set (qy3de.4) ---
	case "/":
		m.typing = true
	case "esc":
		// esc clears the filter and NEVER quits: overloading it to exit
		// means a mistyped `/` drops you out of a full-screen program.
		if m.filter != "" {
			m.filter = ""
			m.fail(m.applyFilter())
		}
	case "C":
		m.scope.includeClosed = !m.scope.includeClosed
		m.fail(m.reload())
	case "a":
		m.scope.allProjects = !m.scope.allProjects
		m.fail(m.reload())

	// --- following (qy3de.6) ---
	case "f":
		m.openFollow()
	case "backspace":
		m.fail(m.popFollow())

	// --- the cursor ---
	case "j", "down":
		m.fail(m.selectRow(m.cursor.position + 1))
	case "k", "up":
		m.fail(m.selectRow(m.cursor.position - 1))
	case "g":
		m.fail(m.selectRow(0))
	case "G":
		m.fail(m.selectRow(len(m.rows) - 1))
	case "ctrl+d":
		m.fail(m.selectRow(m.cursor.position + m.halfPage()))
	case "ctrl+u":
		m.fail(m.selectRow(m.cursor.position - m.halfPage()))

	// --- the detail pane ---
	case "J":
		m.detail.ScrollDown(1)
	case "K":
		m.detail.ScrollUp(1)
	case "space":
		m.detail.PageDown()
	case "b":
		m.detail.PageUp()
	case "h", "left":
		m.detail.ScrollLeft(hscrollStep)
	case "l", "right":
		m.detail.ScrollRight(hscrollStep)
	case "0":
		m.detail.SetXOffset(0)
	case "$":
		m.detail.SetXOffset(m.widest())
	case "w":
		// Soft-wrap on demand. qy3de.3 rejected it as the DEFAULT — a
		// 130-column table row wrapped into four ragged lines at 37 columns
		// is the destruction WrapText refuses to perform — and qy3de.5
		// measured why it earns a key anyway: scrolled to the same row's
		// offset 48, 19 of 21 pane lines are blank and the table's header
		// has gone; wrapped, 3 are blank and every byte reads.
		m.detail.SoftWrap = !m.detail.SoftWrap
		m.detail.SetXOffset(0)
	case "enter":
		// enter drops the two columns for the issue text alone, and brings
		// them back. It is not the wide-line fix — 4.2% of lines still
		// overflow at 78 columns — which is why `w` exists as well.
		m.zoomed = !m.zoomed
		m.resize()
		m.fail(m.showDetail(m.detailID))
	}
	return nil
}

// pickerKey is the whole keymap while a picker is open.
//
// No accelerators and no `/`: the median relation count is 1 and 60 of the
// corpus's 77 non-empty pickers hold three rows or fewer, and `/` one pane
// over already means something entirely different. `g` and `G` are `move` with
// a delta longer than the list, so the 16- and 23-row cases are one press.
//
// ctrl+c still quits, for the same reason it does everywhere else: a
// full-screen program that swallows it is worse than one that does not.
func (m *Model) pickerKey(key string) tea.Cmd {
	rows := m.detailRows(measure(m.width, m.height, m.zoomed))
	switch key {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.follow, m.choices = nil, nil
	case "enter":
		m.pickFollow()
	case "j", "down":
		m.moveFollow(1, rows)
	case "k", "up":
		m.moveFollow(-1, rows)
	case "g":
		m.moveFollow(-len(m.choices), rows)
	case "G":
		m.moveFollow(len(m.choices), rows)
	}
	return nil
}

// moveFollow walks the picker's cursor and re-windows it. move deliberately
// does not know the geometry, so the scroll is a second call rather than a
// height parameter threaded through the component qy3de.7 also reuses.
func (m *Model) moveFollow(delta, rows int) {
	m.follow.move(delta)
	m.follow.scroll(rows)
}

// View composes the frame. Alt screen, window title and content all ride on
// the returned value in bubbletea v2 — there is no tea.WithAltScreen — so they
// are part of the render and visible to a test that never runs a program.
func (m *Model) View() tea.View {
	var view tea.View
	view.SetContent(m.frame())
	view.AltScreen = true
	view.WindowTitle = "drops"
	return view
}

// frame is the whole screen as one string: exactly m.width columns and
// m.height lines, whatever the mode.
//
// The pad here is a guard for a terminal too small to compose a frame in at
// all — under four lines, where the panes cannot fit their own borders. It is
// a no-op at every size above that, which is not an assumption: compose is
// asserted square on its own, so a pane that goes ragged shows up there rather
// than being quietly squared off here.
func (m *Model) frame() string { return pad(m.compose(), m.width, m.height) }

// compose builds the frame: the panes, then the footer.
func (m *Model) compose() string {
	geo := measure(m.width, m.height, m.zoomed)
	rows := m.detailRows(geo)
	m.detail.SetWidth(geo.detailWidth)
	m.detail.SetHeight(rows)

	if m.help {
		return pad(helpText(), m.width, m.height)
	}

	// The picker takes the detail pane's BODY, keeping qy3de.5's box and its
	// border title. A centred overlay needs lipgloss layer compositing that
	// nothing here has built or measured; replacing the body reuses pad/box
	// geometry already proved off-width-zero. Under `enter`-zoom the body is
	// the full width and the picker follows it there.
	inner := pad(m.detail.View(), geo.detailWidth, rows)
	if m.follow != nil {
		inner = m.follow.view(geo.detailWidth, rows)
	}
	if line := m.depthLine(geo.detailWidth); line != "" {
		inner = line + "\n" + inner
	}
	detail := box(pad(inner, geo.detailWidth, geo.rows), geo.detailWidth, m.detailTitle())
	body := detail
	if !m.zoomed {
		list := listBody(m.rows, m.cursor.position, geo.listWidth, geo.rows,
			m.scope.allProjects, m.emptyState())
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			box(list, geo.listWidth, m.listTitle()), detail)
	}
	return body + "\n" + m.footer(geo)
}

// listTitle names the scope the left pane is showing.
func (m *Model) listTitle() string {
	if m.scope.allProjects {
		return "all projects"
	}
	return m.source.project.Slug
}

// detailTitle is the FULL untruncated id. It is the whole justification for
// the left pane's 12-column id cap: the row truncates what identifies, and
// this is where it is recovered.
func (m *Model) detailTitle() string { return string(m.detailID) }

// emptyState distinguishes the two empty panes. Collapsing them makes an empty
// project look like a bad filter.
func (m *Model) emptyState() string {
	if m.filter != "" {
		return "No rows match " + m.filter
	}
	if m.scope.allProjects {
		return "No open issues"
	}
	return "No open issues in " + m.source.project.Slug
}

// widest is the widest line of the rendered page, in columns.
func (m *Model) widest() int {
	widest := 0
	for _, line := range strings.Split(m.pageText, "\n") {
		widest = max(widest, lipgloss.Width(line))
	}
	return widest
}

// footer is qy3de.4's disclosure line on the left and qy3de.5's scroll
// indicator on the right.
//
// Modes read as WORDS because the footer is disclosure: `C` means nothing to
// someone who did not press it. The count is `M of N` while something is
// narrowing and a bare count otherwise, which is one slot that makes both
// `C`'s 6.3x row jump and `/`'s narrowing legible.
func (m *Model) footer(geo geometry) string {
	scope := m.listTitle()
	count := fmt.Sprintf("%d issues", len(m.all))
	if len(m.rows) != len(m.all) {
		count = fmt.Sprintf("%d of %d", len(m.rows), len(m.all))
	}
	parts := []string{scope, count}
	if m.filter != "" || m.typing {
		parts = append(parts, "/"+m.filter)
	}
	if m.scope.includeClosed {
		parts = append(parts, "closed")
	}
	if m.zoomed {
		parts = append(parts, "zoom detail")
	}
	if m.detail.SoftWrap {
		parts = append(parts, "wrap")
	}
	left := strings.Join(parts, " · ")
	if m.notice != "" {
		left = m.notice
	}
	if m.err != nil {
		left = "error: " + m.err.Error()
	}

	right := m.rightSlot(geo)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return pad(dimStyle.Render(left), m.width, 1)
	}
	return dimStyle.Render(left) + strings.Repeat(" ", gap) + dimStyle.Render(right)
}

// rightSlot is the footer's right-hand indicator: where you are in whatever
// the right pane is showing, and only while something is off-screen.
//
// A picker's overflow shows HERE rather than inside the picker (qy3de.6 §6):
// the worst case is 23 rows under 4 headers, 27 rendered lines against a
// 21-row body, and a 22nd row inside the box would cost the row it is
// reporting on. It counts LINES, not rows, because headers take room too.
func (m *Model) rightSlot(geo geometry) string {
	if m.follow != nil {
		rows := m.detailRows(geo)
		if total := m.follow.lineCount(); total > rows {
			return fmt.Sprintf("↕ %d of %d", m.follow.lineOf(m.follow.selected())+1, total)
		}
		return ""
	}

	// The rest is load-bearing rather than decorative: without it a clipped
	// 130-column table row looks like the whole row.
	right := ""
	if widest := m.widest(); widest > geo.detailWidth && !m.detail.SoftWrap {
		right = fmt.Sprintf("↔ %d–%d of %d",
			m.detail.XOffset()+1, m.detail.XOffset()+geo.detailWidth, widest)
	}
	if m.detail.TotalLineCount() > m.detail.VisibleLineCount() {
		if right != "" {
			right += " · "
		}
		right += fmt.Sprintf("%3.0f%%", m.detail.ScrollPercent()*100)
	}
	return right
}

// helpText is `?`. It is the keymap, grouped by what each group acts on.
func helpText() string {
	return strings.Join([]string{
		headStyle.Render("drops tui"),
		"",
		"  j k ↓ ↑ g G ^d ^u   move the list cursor (retargets the detail pane)",
		"  / Esc               filter on id and title · clear the filter",
		"  C a                 include closed · span every project",
		"",
		"  f                   follow a relation: pick one, Enter takes it",
		"  Backspace           back one relation (moving the cursor clears the trail)",
		"",
		"  J K space b         scroll the detail pane vertically",
		"  h l ← → 0 $         scroll the detail pane horizontally",
		"  w                   soft-wrap the detail pane instead of clipping",
		"  enter               the issue text alone, and back",
		"",
		"  ? q                 this help · quit",
	}, "\n")
}
