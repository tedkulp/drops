# What is bubbletea's current API?

Research for `qy3de.2`. Question: what are bubbletea's current version, import path, and
API shape — and does it still cross-compile pure-Go with no cgo?

**Answer: every pin `euv2e.1` recorded on 2026-08-30 holds, and the two questions it did
not ask both have answers.** lipgloss v2 still has **no** border-title API, but the
hand-rolled one `euv2e.5` abandoned works once it is built from `Border` runes and
measured with `lipgloss.Width` — verified below at 30 columns with both an ASCII and a
double-width title. And a `tea.View` ultimately holds a **plain string**, so
`render.Console` composes into a frame through a `bytes.Buffer` with no adapter: the seam
ticket has a choice to make, not a constraint to obey.

Everything below is measured against **this** repo at `316eb51`, Go 1.27.1 (toolchain) /
`go 1.27.0` (module), on linux/amd64, 2026-09-03. The probe is
`internal/scratchtui/main.go`, committed alongside this file on the throwaway
`research/bubbletea-api` branch.

---

## 1. The pins

| module | version | direct |
|---|---|---|
| `charm.land/bubbletea/v2` | v2.0.9 | yes |
| `charm.land/bubbles/v2` | v2.2.1 | yes |
| `charm.land/lipgloss/v2` | v2.0.6 | yes |

**The import path is `charm.land`, confirmed** — that is what resolves and builds. All
three are `charm.land/<pkg>/v2`, not `github.com/charmbracelet/<pkg>`.

Note that the *indirect* dependencies they pull are still `github.com/charmbracelet/*`
(`colorprofile`, `ultraviolet`, `x/ansi`, `x/term`, `x/termios`, `x/windows`). So
`charm.land` is the published module path for the three direct deps only; seeing
`github.com/charmbracelet` in `go.mod` is expected and not a mistake.

## 2. The module cost

| | direct | indirect | total |
|---|---:|---:|---:|
| at `316eb51` | 3 | 10 | 13 |
| with the three charm deps | 6 | 26 | 32 |
| **added** | **3** | **16** | **19** |

**`euv2e.1`'s "19 new modules, 3 direct" is exactly right.** Two smaller corrections to
numbers quoted upstream of this ticket: the ticket says `go.mod` currently holds "3 direct
and 8 indirect" and the map says "a `go.mod` holding 11" — both are **10 indirect, 13
total**. The added count is unaffected.

The 16 new indirect modules: `atotto/clipboard`, `charmbracelet/colorprofile`,
`charmbracelet/ultraviolet`, `charmbracelet/x/ansi`, `charmbracelet/x/term`,
`charmbracelet/x/termios`, `charmbracelet/x/windows`, `clipperhouse/displaywidth`,
`clipperhouse/uax29/v2`, `lucasb-eyer/go-colorful`, `mattn/go-runewidth`,
`muesli/cancelreader`, `rivo/uniseg`, `sahilm/fuzzy`, `xo/terminfo`, `golang.org/x/sync`.

## 3. Pure Go, and it cross-compiles

**Zero C source files** across all three direct deps and the `charmbracelet/*` indirects:
a `find` for `*.c` and `*.h` over their module-cache trees returns nothing.

`CGO_ENABLED=0 go build ./internal/scratchtui` succeeds for **linux/amd64, linux/arm64,
darwin/arm64 and windows/amd64** with no C toolchain. Terminal handling on Unix comes from
`charmbracelet/x/termios` and on Windows from `charmbracelet/x/windows`, both pure Go over
`golang.org/x/sys`.

## 4. The API shape

**`Init` and `Update` are unchanged from v1**, verified by compile-time assertion:

```go
Init() tea.Cmd
Update(tea.Msg) (tea.Model, tea.Cmd)
```

**`View()` returns a `tea.View` struct, not a string.** Terminal modes are *fields on the
returned value* rather than program options — there is no `tea.WithAltScreen()`:

```go
func (m probe) View() tea.View {
	var v tea.View
	v.SetContent(m.compose())   // v.Content is a plain string
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "drops"
	return v
}
```

Probe output:

```
View is a struct: tea.View
View.Content is: string (len 2192)
View.AltScreen is: bool
```

This is the practical consequence: **alt-screen, mouse mode and window title are now part
of the render, so they can change frame to frame** and are visible to a test that inspects
the returned `tea.View` without running a program at all.

Key messages are `tea.KeyPressMsg` (not v1's `tea.KeyMsg`); `msg.String()` and
`key.Matches` both work on it.

## 5. `ExecProcess` releases the terminal

Needed by this map for `$EDITOR`. **Verified by running it**, not by reading the type: a
headless program driven through `tea.WithInput`/`tea.WithOutput` executed
`sh -c 'echo child-ran > <file>'` via `tea.ExecProcess`, and

- the marker file was written, so the child really ran;
- the callback fired with `err=nil`;
- the output stream contains the alt-screen enter **and** exit sequences **twice** each
  (`\x1b[?1049h` / `\x1b[?1049l`) — one pair for the program, one for the exec.

Leaving and re-entering the alt screen around the child is the terminal release.

## 6. Headless programs are testable

Not asked, but it decides how this map tests frames. A real `tea.Program` runs with a pipe
for input and a `bytes.Buffer` for output, at a pinned size:

```go
p := tea.NewProgram(newProbe(),
	tea.WithInput(in), tea.WithOutput(&out), tea.WithWindowSize(100, 30))
```

The 150ms-delayed `q` quit produced 1371 bytes, containing the alt-screen enter/exit, the
OSC-2 window title, the CSI ?1002h mouse enable, and the issue id that came from
`render.Console`. **So golden frames need no terminal and no fake** — which matches this
repo's seam rule, since width and terminal-ness are already constructor parameters on
`render.Console`.

## 7. The string-composition story

**A `tea.View` holds a plain `string`.** `v.Content` is `string`, and `SetContent` takes
one. So `render.Console` writing into a `bytes.Buffer` composes directly into a frame:

```go
var buf bytes.Buffer
console := render.Console{Out: &buf, TTY: false, Width: 40}
console.Page(...)              // existing renderer, unchanged
right := box.Render(buf.String())
frame := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
```

The probe does exactly this and the rendered issue id survives into the emitted frame.
**The seam ticket therefore has a genuine choice**, not a constraint: `render.Console` can
be reused as-is behind a buffer, or the TUI can render independently. Neither is forced by
bubbletea.

## 8. lipgloss border titles: still absent, and the hand-roll now works

**There is no border-title or border-label API in lipgloss v2.0.6.** `Border` is a struct
of edge and corner strings; `Style` has ~50 border methods and none of them takes a label.

`euv2e.5` built one by splicing the title into the rendered top line and dropped it after
hitting an SGR escape and an ambiguous-width rune. **Both failures reproduce exactly.**
Splicing by `[]rune` index into

```
"\x1b[38;5;62m╭────────────────────────────╮\x1b[m"
```

overwrites the *escape sequence*, because the styled line does not start with the corner
rune. And with a double-width title the line's `lipgloss.Width` goes from 30 to **39**.

**The fix, verified.** Build the top edge from the `Border` runes, measure the label with
`lipgloss.Width`, colour the finished edge in one `Style`, and render the body with its
top border switched off:

```go
b := lipgloss.RoundedBorder()
fill := width - 2 - lead - lipgloss.Width(" "+title+" ")
top := b.TopLeft + strings.Repeat(b.Top, lead) + " " + title + " " +
	strings.Repeat(b.Top, fill) + b.TopRight
edge := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Render(top)
rest := lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder(), false, true, true, true).
	BorderForeground(lipgloss.Color("62")).
	Width(width).
	Render(body)
return edge + "\n" + rest
```

Every line measures 30 columns with title `"detail"` **and** with title `"詳細"`:

```
╭── detail ──────────────────╮      ╭── 詳細 ────────────────────╮
│body                        │      │body                        │
╰────────────────────────────╯      ╰────────────────────────────╯
  line 0 width 30                     line 0 width 30
  line 1 width 30                     line 1 width 30
  line 2 width 30                     line 2 width 30
```

**The trap that made the probe's first attempt off by two:** in lipgloss v2,
`Style.Width(n)` sets the **total** rendered width, borders included — not the content
width. Measured directly:

```
Width(10): no border -> 10, with border -> 10
Width(20): no border -> 20, with border -> 20
Width(30): no border -> 30, with border -> 30
```

So the body must be `Width(width)`, not `Width(width-2)`. Getting this backwards yields a
top edge and a body that differ by exactly two columns, which is what the probe's
`border-title-fixed` mode still prints (30 / 28 / 28).

## 9. What this does not answer

- **Whether the TUI should reuse `render.Console`.** Section 7 says it *can*; whether it
  *should* is `qy3de.3`.
- **Colour, `--no-color` and `NO_COLOR`.** Untouched here, and still fog on the map.
  lipgloss v2 dropping `AdaptiveColor` (so dark/light needs a
  `tea.RequestBackgroundColor` round trip) is `euv2e.1`'s finding and was not re-verified.
- **Whether `ExecProcess` behaves on a real terminal.** Section 5 proves the child runs and
  the alt screen cycles in a headless program. Real-terminal behaviour is a build-time
  observation for `qy3de.5`.
- **Performance.** No timing was taken; poll cost at scale is still fog.

## Sources

- Module cache at `$(go env GOMODCACHE)` for `charm.land/{bubbletea,bubbles,lipgloss}/v2`
  and `github.com/charmbracelet/*`, as resolved by `go mod tidy` on 2026-09-03.
- `go doc charm.land/lipgloss/v2`, `go doc charm.land/lipgloss/v2.Border`,
  `go doc charm.land/lipgloss/v2.Style`.
- `internal/scratchtui/main.go`, modes `types`, `headless`, `border-title`,
  `border-title-fixed`.
- `euv2e.1` (the 2026-08-30 pins) and `euv2e.5` (the abandoned border label), read as
  claims to re-verify rather than as facts.
