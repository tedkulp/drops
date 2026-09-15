package tui

// PROTOTYPE — THROWAWAY. This file answers tedkulp/drops#2 ("How does the
// board look: columns, cards, and the detail pane?") and lives only on the
// prototype/board-look branch. It is not the board, carries no tests, and
// should never be merged. Run it with `just board-prototype`.
//
// Five structurally different layouts over the same loaded cards:
//
//	[ ]   cycle the layout variant (the floating switcher's terminal stand-in)
//	d     cycle card density: 1, 2, 3 or 4 lines (4 is bv's card)
//	< >   force the frame to 80, 120 or 200 columns, or the real width
//
// plus enough of the decided keymap (#5) to feel it: tab/shift+tab between
// columns, j k g G ^d ^u within one, r for ready-only. Moves, follow, x and
// the poll are deliberately absent — #3 and #5 already settled them.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

type protoCard struct {
	row
	kind       model.IssueType
	assignee   string
	labels     []string
	downstream int
	age        string
	at         time.Time
}

const (
	colReady = iota
	colBlocked
	colProgress
	colClosed
)

var protoColNames = [4]string{"Ready", "Blocked", "In progress", "Closed"}

var protoVariants = []struct{ key, name string }{
	{"A", "shrink: four columns in the list's half"},
	{"B", "accordion: focused column wide, the rest strips"},
	{"C", "stacked: board full width over the detail"},
	{"D", "grouped: the list pane with column headings"},
	{"E", "paged: min-width columns, scroll sideways"},
}

var protoWidths = []int{0, 80, 120, 200}

type boardProto struct {
	ctx    context.Context
	source *source

	all       [4][]protoCard
	readyOnly bool

	variant, density, widthIx int
	col                       int
	sel, top                  [4]int
	listTop, first            int

	width, height int
	pages         map[string]string
	err           error
}

// RunBoardPrototype runs the throwaway board prototype full screen.
func RunBoardPrototype(ctx context.Context, issues *core.Core, project model.Project, variant string) error {
	b := newBoardProto(ctx, issues, project, variant, 2)
	if err := b.load(); err != nil {
		return err
	}
	_, err := tea.NewProgram(b, tea.WithContext(ctx)).Run()
	return err
}

// DumpBoardPrototype renders one frame as plain text, for comparing variants
// without a terminal.
func DumpBoardPrototype(ctx context.Context, issues *core.Core, project model.Project,
	width, height int, variant string, density, col, sel int) (string, error) {
	b := newBoardProto(ctx, issues, project, variant, density)
	if err := b.load(); err != nil {
		return "", err
	}
	b.width, b.height, b.col = width, height, col
	b.sel[col] = sel
	return ansi.Strip(b.frame()), nil
}

func newBoardProto(ctx context.Context, issues *core.Core, project model.Project, variant string, density int) *boardProto {
	b := &boardProto{
		ctx:     ctx,
		source:  &source{core: issues, project: project},
		density: density,
		width:   render.DefaultWidth,
		height:  24,
		pages:   map[string]string{},
	}
	for index, v := range protoVariants {
		if strings.EqualFold(v.key, variant) {
			b.variant = index
		}
	}
	return b
}

func (b *boardProto) load() error {
	key := b.source.project.Key
	issues, err := b.source.core.Issues(b.ctx, core.IssueFilter{Project: &key,
		Statuses: []model.Status{model.StatusOpen, model.StatusInProgress, model.StatusClosed}})
	if err != nil {
		return err
	}
	blockers, err := b.source.core.OpenBlockers(b.ctx)
	if err != nil {
		return err
	}
	ids := make([]model.ID, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	impacts, err := b.source.core.UnblockImpacts(b.ctx, ids)
	if err != nil {
		return err
	}

	now := time.Now()
	var cols [4][]protoCard
	for _, issue := range issues {
		// One read per card: core has no bulk label read yet (#4). Fine for
		// a prototype, and exactly what the map says the TUI may not do.
		labels, err := b.source.core.IssueLabels(b.ctx, issue.ID)
		if err != nil {
			return err
		}
		card := protoCard{
			row: row{id: issue.ID, status: issue.Status, priority: issue.Priority,
				title: issue.Title, blockedBy: len(blockers[issue.ID])},
			kind:       issue.Type,
			downstream: impacts[issue.ID],
		}
		for _, label := range labels {
			if label.Tombstone != model.Tombstoned {
				card.labels = append(card.labels, label.Name)
			}
		}
		if issue.Assignee != nil {
			card.assignee = *issue.Assignee
		}
		since := issue.CreatedAt
		column := colReady
		switch {
		case issue.Status == model.StatusClosed:
			column = colClosed
			if issue.ClosedAt != nil {
				since = *issue.ClosedAt
			}
		case issue.Status == model.StatusInProgress:
			column = colProgress
			if issue.StartedAt != nil {
				since = *issue.StartedAt
			}
		case card.blockedBy > 0:
			column = colBlocked
		}
		card.at, _ = since.Time()
		card.age = protoAge(now.Sub(card.at))
		cols[column] = append(cols[column], card)
	}
	sort.SliceStable(cols[colClosed], func(i, j int) bool {
		return cols[colClosed][i].at.After(cols[colClosed][j].at)
	})
	b.all = cols
	b.pages = map[string]string{}
	return nil
}

func protoAge(d time.Duration) string {
	day := 24 * time.Hour
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*day:
		return fmt.Sprintf("%dd", int(d/day))
	case d < 365*day:
		return fmt.Sprintf("%dmo", int(d/(30*day)))
	}
	return fmt.Sprintf("%dy", int(d/(365*day)))
}

// visible applies `r`: the same blockedBy == 0 predicate, so Blocked empties
// and blocked In progress cards go too (#5).
func (b *boardProto) visible() [4][]protoCard {
	cols := b.all
	if b.readyOnly {
		cols[colBlocked] = nil
		kept := []protoCard{}
		for _, card := range cols[colProgress] {
			if card.blockedBy == 0 {
				kept = append(kept, card)
			}
		}
		cols[colProgress] = kept
	}
	for c := range cols {
		b.sel[c] = max(0, min(b.sel[c], len(cols[c])-1))
	}
	return cols
}

func (b *boardProto) current() (protoCard, bool) {
	cols := b.visible()
	if len(cols[b.col]) == 0 {
		return protoCard{}, false
	}
	return cols[b.col][b.sel[b.col]], true
}

// --- cards ---------------------------------------------------------------

// protoFit puts right flush against the right edge and cuts left to fit.
func protoFit(left, right string, width int) string {
	if right == "" {
		return render.Ellipsis(left, width)
	}
	room := width - render.Cols(right) - 1
	if room < 1 {
		return render.Ellipsis(right, width)
	}
	left = render.Ellipsis(left, room)
	return left + strings.Repeat(" ", room-render.Cols(left)) + " " + right
}

func (c protoCard) lines(width, density int) []string {
	mark := render.StatusMark(c.status, c.tombstoned)
	blocked := ""
	if c.blockedBy > 0 {
		blocked = fmt.Sprintf("[%d]", c.blockedBy)
	}
	if density == 1 {
		return []string{protoFit(fmt.Sprintf("%s P%d %s %s", mark, c.priority, c.id, c.title), blocked, width)}
	}

	meta := fmt.Sprintf("%s P%d %s %s", mark, c.priority, c.kind, c.id)
	right := c.age
	if density == 2 && blocked != "" {
		right = blocked + " " + c.age
	}
	head := protoFit(meta, right, width)
	title := render.Ellipsis(c.title, width)
	if density == 2 {
		return []string{head, title}
	}

	var facts []string
	if c.assignee != "" {
		facts = append(facts, "@"+c.assignee)
	}
	if c.blockedBy > 0 {
		facts = append(facts, fmt.Sprintf("blocked by %d", c.blockedBy))
	}
	if c.downstream > 0 {
		facts = append(facts, fmt.Sprintf("unblocks %d", c.downstream))
	}
	tags := ""
	if len(c.labels) > 0 {
		tags = "#" + strings.Join(c.labels, " #")
	}
	if density == 3 {
		if tags != "" {
			facts = append(facts, tags)
		}
		return []string{head, title, render.Ellipsis(strings.Join(facts, " · "), width)}
	}
	if len(c.labels) > 0 {
		facts = append(facts, fmt.Sprintf("%d labels", len(c.labels)))
	}
	return []string{head, title,
		render.Ellipsis(strings.Join(facts, " · "), width),
		render.Ellipsis(tags, width)}
}

// --- columns -------------------------------------------------------------

func protoTop(top, sel, fit, total int) int {
	if sel < top {
		top = sel
	}
	if sel >= top+fit {
		top = sel - fit + 1
	}
	return max(0, min(top, total-fit))
}

func (b *boardProto) emptyText(c int) string {
	switch {
	case c == colBlocked && b.readyOnly:
		return "r to show blocked"
	case c == colClosed:
		return "nothing closed"
	}
	return "none"
}

func (b *boardProto) column(c int, cards []protoCard, width, height int) string {
	focused := c == b.col
	header := fmt.Sprintf("%s %d", protoColNames[c], len(cards))
	gap := 0
	if b.density > 1 {
		gap = 1
	}
	fit := max((height-1+gap)/(b.density+gap), 1)
	b.top[c] = protoTop(b.top[c], b.sel[c], fit, len(cards))
	top := b.top[c]
	if len(cards) > fit {
		header += fmt.Sprintf(" · %d–%d", top+1, min(top+fit, len(cards)))
	}
	if focused {
		header = headStyle.Render(render.Ellipsis("▸ "+header, width))
	} else {
		header = dimStyle.Render(render.Ellipsis("  "+header, width))
	}

	lines := []string{header}
	if len(cards) == 0 {
		lines = append(lines, dimStyle.Render(render.Ellipsis("  "+b.emptyText(c), width)))
	}
	for i := top; i < len(cards) && i < top+fit; i++ {
		if i > top && gap > 0 {
			lines = append(lines, "")
		}
		for _, line := range cards[i].lines(width, b.density) {
			line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
			switch {
			case focused && i == b.sel[c]:
				line = cursorStyle.Render(line)
			case cards[i].status == model.StatusClosed:
				line = dimStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}
	return pad(strings.Join(lines, "\n"), width, height)
}

// strip is variant B's collapsed column: a mark, a priority and an id.
func (b *boardProto) strip(c int, cards []protoCard, width, height int) string {
	abbrev := [4]string{"Rdy", "Blk", "Prg", "Cls"}
	lines := []string{dimStyle.Render(render.Ellipsis(fmt.Sprintf("%s %d", abbrev[c], len(cards)), width))}
	fit := max(height-1, 1)
	b.top[c] = protoTop(b.top[c], b.sel[c], fit, len(cards))
	if len(cards) == 0 {
		lines = append(lines, dimStyle.Render("·"))
	}
	for i := b.top[c]; i < len(cards) && i < b.top[c]+fit; i++ {
		card := cards[i]
		lines = append(lines, dimStyle.Render(render.Ellipsis(
			fmt.Sprintf("%s%d %s", render.StatusMark(card.status, false), card.priority, card.id), width)))
	}
	return pad(strings.Join(lines, "\n"), width, height)
}

func protoJoin(blocks []string, height int) string {
	split := make([][]string, len(blocks))
	for i, block := range blocks {
		split[i] = strings.Split(block, "\n")
	}
	sep := dimStyle.Render("│")
	out := make([]string, 0, height)
	for line := 0; line < height; line++ {
		parts := make([]string, len(blocks))
		for i := range blocks {
			if line < len(split[i]) {
				parts[i] = split[i][line]
			}
		}
		out = append(out, strings.Join(parts, sep))
	}
	return strings.Join(out, "\n")
}

func (b *boardProto) detail(width, rows int) string {
	card, ok := b.current()
	if !ok {
		return box(pad(dimStyle.Render("no card"), width, rows), width, "")
	}
	cacheKey := fmt.Sprintf("%s@%d", card.id, width)
	text, cached := b.pages[cacheKey]
	if !cached {
		issueView, err := b.source.issue(b.ctx, card.id)
		if err != nil {
			b.err = err
			return box(pad("", width, rows), width, "")
		}
		if text, err = b.source.page(issueView, width); err != nil {
			b.err = err
		}
		b.pages[cacheKey] = text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return box(pad(strings.Join(lines, "\n"), width, rows), width, string(card.id))
}

// --- the five variants ---------------------------------------------------

func (b *boardProto) boardTitle(extra string) string {
	title := b.source.project.Slug + " · board"
	if extra != "" {
		title += " · " + extra
	}
	return title
}

// A: the board takes the list pane's half, four equal columns inside it.
func (b *boardProto) shrink(cols [4][]protoCard, width, height int) (string, string) {
	outerLeft := int(float64(width) * split)
	boardW, detailW, rows := outerLeft-2, width-outerLeft-2, height-2
	colW := max((boardW-3)/4, 1)
	blocks := make([]string, 4)
	for c := range 4 {
		w := colW
		if c == 3 {
			w = max(boardW-3-3*colW, 1)
		}
		blocks[c] = b.column(c, cols[c], w, rows)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		box(protoJoin(blocks, rows), boardW, b.boardTitle("")), b.detail(detailW, rows))
	return body, fmt.Sprintf("board %d · columns %d · detail %d", boardW, colW, detailW)
}

// B: same half, but only the focused column is wide.
func (b *boardProto) accordion(cols [4][]protoCard, width, height int) (string, string) {
	outerLeft := int(float64(width) * split)
	boardW, detailW, rows := outerLeft-2, width-outerLeft-2, height-2
	stripW := 10
	if boardW-3-3*stripW < 24 {
		stripW = max((boardW-3-24)/3, 3)
	}
	focusW := max(boardW-3-3*stripW, 1)
	blocks := make([]string, 4)
	for c := range 4 {
		if c == b.col {
			blocks[c] = b.column(c, cols[c], focusW, rows)
		} else {
			blocks[c] = b.strip(c, cols[c], stripW, rows)
		}
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		box(protoJoin(blocks, rows), boardW, b.boardTitle("")), b.detail(detailW, rows))
	return body, fmt.Sprintf("board %d · focused %d · strips %d · detail %d", boardW, focusW, stripW, detailW)
}

// C: the board spans the frame; the detail pane sits underneath it.
func (b *boardProto) stacked(cols [4][]protoCard, width, height int) (string, string) {
	boardH := height * 55 / 100
	detailH := height - boardH
	boardW := width - 2
	colW := max((boardW-3)/4, 1)
	blocks := make([]string, 4)
	for c := range 4 {
		w := colW
		if c == 3 {
			w = max(boardW-3-3*colW, 1)
		}
		blocks[c] = b.column(c, cols[c], w, boardH-2)
	}
	body := box(protoJoin(blocks, boardH-2), boardW, b.boardTitle("")) + "\n" +
		b.detail(width-2, max(detailH-2, 1))
	return body, fmt.Sprintf("board %d×%d · columns %d · detail %d×%d", boardW, boardH-2, colW, width-2, detailH-2)
}

// D: no columns at all; the left pane is the list, sectioned by column.
func (b *boardProto) grouped(cols [4][]protoCard, width, height int) (string, string) {
	outerLeft := int(float64(width) * split)
	listW, detailW, rows := outerLeft-2, width-outerLeft-2, height-2

	var lines []string
	selLine := 0
	for c := range 4 {
		head := fmt.Sprintf("── %s %d ", protoColNames[c], len(cols[c]))
		if c == b.col {
			head = "▸─" + strings.TrimPrefix(head, "──")
		}
		head += strings.Repeat("─", max(0, listW-render.Cols(head)))
		if c == b.col {
			lines = append(lines, headStyle.Render(head))
		} else {
			lines = append(lines, dimStyle.Render(head))
		}
		if len(cols[c]) == 0 {
			lines = append(lines, dimStyle.Render(render.Ellipsis("  "+b.emptyText(c), listW)))
			if c == b.col {
				selLine = len(lines) - 1
			}
		}
		for i, card := range cols[c] {
			selected := c == b.col && i == b.sel[c]
			if selected {
				selLine = len(lines)
			}
			for _, line := range card.lines(listW, b.density) {
				line += strings.Repeat(" ", max(0, listW-lipgloss.Width(line)))
				switch {
				case selected:
					line = cursorStyle.Render(line)
				case card.status == model.StatusClosed:
					line = dimStyle.Render(line)
				}
				lines = append(lines, line)
			}
		}
		if c < 3 {
			lines = append(lines, "")
		}
	}
	top := b.listTop
	if selLine < top {
		top = selLine
	}
	if selLine+b.density > top+rows {
		top = selLine + b.density - rows
	}
	top = max(0, min(top, len(lines)-rows))
	b.listTop = top
	list := strings.Join(lines[top:min(top+rows, len(lines))], "\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		box(pad(list, listW, rows), listW, b.boardTitle(fmt.Sprintf("line %d of %d", top+1, len(lines)))),
		b.detail(detailW, rows))
	return body, fmt.Sprintf("list %d · detail %d · %d lines", listW, detailW, len(lines))
}

// E: columns never go below minColW; what does not fit scrolls sideways,
// always keeping the focused column on screen. The detail pane yields to 40%.
func (b *boardProto) paged(cols [4][]protoCard, width, height int) (string, string) {
	const minColW = 28
	detailOuter := max(width*2/5, 38)
	boardW := max(width-detailOuter-2, 1)
	detailW, rows := width-boardW-4, height-2
	n := max(1, min(4, (boardW+1)/(minColW+1)))
	if b.col < b.first {
		b.first = b.col
	}
	if b.col >= b.first+n {
		b.first = b.col - n + 1
	}
	b.first = max(0, min(b.first, 4-n))
	colW := max((boardW-(n-1))/n, 1)
	blocks := make([]string, 0, n)
	for c := b.first; c < b.first+n; c++ {
		w := colW
		if c == b.first+n-1 {
			w = max(boardW-(n-1)-(n-1)*colW, 1)
		}
		blocks = append(blocks, b.column(c, cols[c], w, rows))
	}
	extra := ""
	if n < 4 {
		extra = fmt.Sprintf("◀ %d · %d ▶", b.first, 4-b.first-n)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		box(protoJoin(blocks, rows), boardW, b.boardTitle(extra)), b.detail(detailW, rows))
	return body, fmt.Sprintf("board %d · %d of 4 columns at %d · detail %d", boardW, n, colW, detailW)
}

// --- frame ---------------------------------------------------------------

func (b *boardProto) frameWidth() int {
	if w := protoWidths[b.widthIx]; w > 0 {
		return w
	}
	return b.width
}

func (b *boardProto) frame() string {
	width := b.frameWidth()
	height := max(b.height, 12)
	bodyH := height - 3
	cols := b.visible()

	var body, geom string
	switch b.variant {
	case 0:
		body, geom = b.shrink(cols, width, bodyH)
	case 1:
		body, geom = b.accordion(cols, width, bodyH)
	case 2:
		body, geom = b.stacked(cols, width, bodyH)
	case 3:
		body, geom = b.grouped(cols, width, bodyH)
	default:
		body, geom = b.paged(cols, width, bodyH)
	}

	total := 0
	for _, cards := range cols {
		total += len(cards)
	}
	parts := []string{b.source.project.Slug, "board", fmt.Sprintf("%d issues", total)}
	if b.readyOnly {
		parts = append(parts, "ready")
	}
	footer := dimStyle.Render(strings.Join(parts, " · "))
	if b.err != nil {
		footer = "error: " + b.err.Error()
	}

	card, _ := b.current()
	state := fmt.Sprintf("state · column %s · card %d/%d %s · %s · R:%d B:%d I:%d C:%d",
		protoColNames[b.col], b.sel[b.col]+1, len(cols[b.col]), card.id, geom,
		len(cols[0]), len(cols[1]), len(cols[2]), len(cols[3]))

	forced := "terminal"
	if protoWidths[b.widthIx] > 0 {
		forced = "forced"
	}
	v := protoVariants[b.variant]
	bar := cursorStyle.Render(fmt.Sprintf(
		" PROTOTYPE #2  ◀ [ %s · %s ] ▶   %d-line cards   %d cols (%s)   ·  [ ] variant  d cards  < > width  tab col  r ready  q quit ",
		v.key, v.name, b.density, width, forced))

	frame := body + "\n" + footer + "\n" + dimStyle.Render(state) + "\n" + bar
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		line = ansi.Truncate(line, b.width, "")
		lines[i] = line + strings.Repeat(" ", max(0, b.width-lipgloss.Width(line)))
	}
	return strings.Join(lines, "\n")
}

// --- bubbletea -----------------------------------------------------------

func (b *boardProto) Init() tea.Cmd { return nil }

func (b *boardProto) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return b, b.key(msg.String())
	}
	return b, nil
}

func (b *boardProto) key(key string) tea.Cmd {
	cols := b.visible()
	last := len(cols[b.col]) - 1
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "]":
		b.variant = (b.variant + 1) % len(protoVariants)
	case "[":
		b.variant = (b.variant + len(protoVariants) - 1) % len(protoVariants)
	case "d":
		b.density = b.density%4 + 1
	case ">":
		b.widthIx = (b.widthIx + 1) % len(protoWidths)
	case "<":
		b.widthIx = (b.widthIx + len(protoWidths) - 1) % len(protoWidths)
	case "tab", "shift+tab":
		step := 1
		if key == "shift+tab" {
			step = 3
		}
		from, to := b.col, (b.col+step)%4
		// Land on the card nearest the old row position (#5).
		b.sel[to] = b.top[to] + (b.sel[from] - b.top[from])
		b.col = to
	case "j", "down":
		b.sel[b.col] = min(b.sel[b.col]+1, last)
	case "k", "up":
		b.sel[b.col]--
	case "g":
		b.sel[b.col] = 0
	case "G":
		b.sel[b.col] = last
	case "ctrl+d":
		b.sel[b.col] = min(b.sel[b.col]+10, last)
	case "ctrl+u":
		b.sel[b.col] -= 10
	case "r":
		b.readyOnly = !b.readyOnly
	case "R":
		if err := b.load(); err != nil {
			b.err = err
		}
	}
	b.visible()
	return nil
}

func (b *boardProto) View() tea.View {
	var view tea.View
	view.SetContent(b.frame())
	view.AltScreen = true
	view.WindowTitle = "drops board PROTOTYPE"
	return view
}
