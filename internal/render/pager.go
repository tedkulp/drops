package render

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

// DefaultPagerCommand is the pager to run when the caller has no preference.
//
// The `less` flags matter: -F exits immediately when the output fits one
// screen, so a thin issue still prints inline rather than trapping the reader
// in a pager; -X leaves that output on the screen instead of wiping it with
// the alternate screen; -R passes escapes through for whenever this gains
// colour.
const DefaultPagerCommand = "less -FRX"

// CommandPager runs an external pager as a child process. It is the real
// adapter behind Pager; a zero value pages nothing and writes straight
// through, which is what an explicitly empty $PAGER asks for.
type CommandPager struct {
	// Command is the pager command line, split on whitespace into a program
	// and its arguments. Empty disables paging.
	Command string
	// Stderr is where the pager's own diagnostics go. Nil means os.Stderr.
	Stderr io.Writer
}

// Page runs render against the pager's stdin, and against out when there is no
// pager to run.
//
// A pager that cannot start is not an error: the output goes straight to out
// instead. The render error is held rather than returned early, because the
// pipe still has to close and the child still has to be reaped, or the
// terminal is left to a process nobody is waiting on.
func (pager CommandPager) Page(out io.Writer, render func(io.Writer) error) error {
	fields := strings.Fields(pager.Command)
	if len(fields) == 0 {
		return render(out)
	}

	command := exec.Command(fields[0], fields[1:]...)
	command.Stdout = out
	command.Stderr = pager.Stderr
	if command.Stderr == nil {
		command.Stderr = os.Stderr
	}
	pipe, err := command.StdinPipe()
	if err != nil {
		return render(out)
	}
	if err := command.Start(); err != nil {
		return render(out)
	}
	renderErr := render(pipe)
	_ = pipe.Close()
	_ = command.Wait()
	return renderErr
}
