package cli

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/tedkulp/drops/internal/render"
	"golang.org/x/sys/unix"
)

// isTerminal reports whether f is a character device, i.e. an interactive
// terminal. drops is UNIX-only, so this is the ioctl path, never an
// exec("tty").
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}

// renderWidth is the column budget for wrapping. $COLUMNS wins when set, so a
// caller can pin the width without a terminal; otherwise a real terminal is
// measured, and anything else gets the fixed default. Matches render's
// Default/Min/Max contract exactly.
func renderWidth(out io.Writer) int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > render.MinWidth {
			return min(n, render.MaxWidth)
		}
	}
	f, ok := out.(*os.File)
	if !ok || !isTerminal(f) {
		return render.DefaultWidth
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col < render.MinWidth {
		return render.DefaultWidth
	}
	return min(int(ws.Col), render.MaxWidth)
}

// pagerCommand is the pager command line, or "" to disable paging. $PAGER
// wins; an explicitly empty $PAGER disables paging, which is how a caller
// turns it off without a flag.
func pagerCommand() string {
	if p, set := os.LookupEnv("PAGER"); set {
		if strings.TrimSpace(p) == "" {
			return ""
		}
		return p
	}
	return render.DefaultPagerCommand
}

// newConsole builds the Console a human-surface verb renders through. A
// non-terminal writer (a pipe, a test buffer) gets no TTY and no pager, so
// truncation is off and every byte prints — the rule that keeps
// `drops list | grep` lossless.
func newConsole(out, errOut io.Writer) render.Console {
	console := render.Console{Out: out, Width: renderWidth(out)}
	f, ok := out.(*os.File)
	if ok && isTerminal(f) {
		console.TTY = true
		if cmd := pagerCommand(); cmd != "" {
			console.Pager = render.CommandPager{Command: cmd, Stderr: errOut}
		}
	}
	return console
}
