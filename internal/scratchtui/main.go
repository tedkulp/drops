// Package main is a throwaway probe for qy3de.2. Not shipped.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// item is a bubbles/list row.
type item struct{ id, title string }

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.id }
func (i item) FilterValue() string { return i.title }

type probe struct {
	rows    list.Model
	detail  viewport.Model
	quitKey key.Binding
	edited  bool
	frames  int
}

// Init: v1 signature, Init() tea.Cmd.
func (m probe) Init() tea.Cmd { return nil }

// Update: v1 signature, Update(tea.Msg) (tea.Model, tea.Cmd).
func (m probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.detail.SetWidth(msg.Width / 2)
		m.detail.SetHeight(msg.Height - 2)
	case editedMsg:
		m.edited = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		if key.Matches(msg, m.quitKey) {
			return m, tea.Quit
		}
		if msg.String() == "e" {
			return m, tea.ExecProcess(exec.Command("true"), func(err error) tea.Msg {
				return editedMsg{err}
			})
		}
	}
	var cmd tea.Cmd
	m.rows, cmd = m.rows.Update(msg)
	return m, cmd
}

type editedMsg struct{ err error }

// View: returns tea.View, not string.
func (m probe) View() tea.View {
	var v tea.View
	v.SetContent(m.compose())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "drops"
	return v
}

// compose builds the frame as a plain string: render.Console into a
// bytes.Buffer, then lipgloss around it.
func (m probe) compose() string {
	var buf bytes.Buffer
	console := render.Console{Out: &buf, TTY: false, Width: 40}
	err := console.Page(render.Page{
		ID:          model.ID("qy3de.2"),
		Project:     "drops",
		Title:       "What is bubbletea's current API?",
		Status:      model.Status("open"),
		Type:        model.IssueType("task"),
		Priority:    2,
		Assignee:    "research-agent",
		CreatedAt:   model.Timestamp("2026-09-03T00:00:00Z"),
		UpdatedAt:   model.Timestamp("2026-09-03T00:00:00Z"),
		Description: "A body wide enough that Wrap has to do work, with a CJK 幅 rune in it.",
	})
	if err != nil {
		return "render error: " + err.Error()
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(44)

	left := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Width(30).Render(m.rows.View())
	right := box.Render(buf.String())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// spliceTitle is the hand-rolled border label euv2e.5 abandoned. Kept to
// measure what breaks.
func spliceTitle(rendered, title string) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}
	top := lines[0]
	runes := []rune(top)
	label := []rune(" " + title + " ")
	if len(runes) < len(label)+4 {
		return rendered
	}
	copy(runes[2:], label)
	lines[0] = string(runes)
	return strings.Join(lines, "\n")
}

func newProbe() probe {
	items := []list.Item{
		item{"qy3de.1", "Stage the real corpus in a scratch store"},
		item{"qy3de.2", "What is bubbletea's current API?"},
		item{"qy3de.3", "Where does the TUI sit?"},
	}
	l := list.New(items, list.NewDefaultDelegate(), 30, 10)
	l.Title = "drops"
	return probe{
		rows:    l,
		detail:  viewport.New(),
		quitKey: key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}

func main() {
	switch os.Args[1] {
	case "types":
		// Compile-time proof of shapes.
		var m tea.Model = newProbe()
		v := m.View()
		var _ string = v.Content
		var _ bool = v.AltScreen
		fmt.Printf("View is a struct: %T\n", v)
		fmt.Printf("View.Content is: %T (len %d)\n", v.Content, len(v.Content))
		fmt.Printf("View.AltScreen is: %T\n", v.AltScreen)
		var _ tea.Cmd = m.Init()
		_, _ = m.Update(tea.KeyPressMsg{})
		fmt.Println("Init() tea.Cmd and Update(tea.Msg) (tea.Model, tea.Cmd) both satisfied")
		var _ tea.Cmd = tea.ExecProcess(exec.Command("true"), nil)
		fmt.Println("tea.ExecProcess exists and returns a tea.Cmd")

	case "frame":
		fmt.Print(newProbe().compose())
		fmt.Println()

	case "border-title":
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Width(30).
			Render("body")
		fmt.Printf("plain splice:\n%s\n", spliceTitle(box, "detail"))
		fmt.Printf("wide-rune splice:\n%s\n", spliceTitle(box, "詳細"))
		fmt.Printf("raw first line bytes: %q\n", strings.Split(box, "\n")[0])
		fmt.Printf("lipgloss.Width(top) = %d\n", lipgloss.Width(strings.Split(box, "\n")[0]))
		fmt.Printf("lipgloss.Width(spliced wide) = %d\n", lipgloss.Width(strings.Split(spliceTitle(box, "詳細"), "\n")[0]))

	case "border-title-fixed":
		// Build the top edge from the Border runes and the title, measure
		// with display width, then colour the whole edge in one Style.
		b := lipgloss.RoundedBorder()
		const w = 30
		title := "詳細"
		lead := 2
		labelw := lipgloss.Width(" " + title + " ")
		fill := w - 2 - lead - labelw
		if fill < 0 {
			fill = 0
		}
		top := b.TopLeft + strings.Repeat(b.Top, lead) + " " + title + " " + strings.Repeat(b.Top, fill) + b.TopRight
		edge := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(top)
		body := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, true, true, true).
			BorderForeground(lipgloss.Color("62")).
			Width(w - 2).
			Render("body")
		out := edge + "\n" + body
		fmt.Println(out)
		fmt.Printf("top edge display width = %d (want %d)\n", lipgloss.Width(edge), w)
		for i, line := range strings.Split(out, "\n") {
			fmt.Printf("  line %d width %d\n", i, lipgloss.Width(line))
		}

	case "headless":
		// Run a real Program with a pipe for input and a buffer for output.
		in, w, _ := os.Pipe()
		var out bytes.Buffer
		p := tea.NewProgram(newProbe(),
			tea.WithInput(in),
			tea.WithOutput(&out),
			tea.WithWindowSize(100, 30),
		)
		go func() {
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte("q"))
		}()
		final, err := p.Run()
		if err != nil {
			fmt.Println("run error:", err)
			os.Exit(1)
		}
		got := out.String()
		fmt.Printf("program ran, final model %T, %d bytes emitted\n", final, len(got))
		fmt.Printf("alt-screen enter (CSI ?1049h) present: %v\n", strings.Contains(got, "\x1b[?1049h"))
		fmt.Printf("alt-screen exit  (CSI ?1049l) present: %v\n", strings.Contains(got, "\x1b[?1049l"))
		fmt.Printf("window title (OSC 2) present: %v\n", strings.Contains(got, "\x1b]2;drops"))
		fmt.Printf("mouse cell-motion (CSI ?1002h) present: %v\n", strings.Contains(got, "\x1b[?1002h"))
		fmt.Printf("issue id from render.Console present in frame: %v\n", strings.Contains(got, "qy3de.2"))
	}
}
