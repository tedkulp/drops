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

	detail   viewport.Model
	detailID model.ID
	pageText string

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
	text, err := m.source.page(m.ctx, id, geo.detailWidth)
	if err != nil {
		return err
	}
	m.detailID, m.pageText = id, text
	m.detail.SetContent(text)
	m.detail.SetYOffset(0)
	m.detail.SetXOffset(0)
	return nil
}

// selectRow moves the cursor and retargets the detail pane with it.
func (m *Model) selectRow(to int) error {
	m.cursor.move(m.rows, to)
	return m.showDetail(m.cursor.id)
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
	m.detail.SetHeight(geo.rows)
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
	m.detail.SetWidth(geo.detailWidth)
	m.detail.SetHeight(geo.rows)

	if m.help {
		return pad(helpText(), m.width, m.height)
	}

	detail := box(pad(m.detail.View(), geo.detailWidth, geo.rows), geo.detailWidth, m.detailTitle())
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
	if m.err != nil {
		left = "error: " + m.err.Error()
	}

	// The right half is load-bearing rather than decorative: without it a
	// clipped 130-column table row looks like the whole row.
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

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return pad(dimStyle.Render(left), m.width, 1)
	}
	return dimStyle.Render(left) + strings.Repeat(" ", gap) + dimStyle.Render(right)
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
		"  J K space b         scroll the detail pane vertically",
		"  h l ← → 0 $         scroll the detail pane horizontally",
		"  w                   soft-wrap the detail pane instead of clipping",
		"  enter               the issue text alone, and back",
		"",
		"  ? q                 this help · quit",
	}, "\n")
}
