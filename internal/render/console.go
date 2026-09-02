package render

import "io"

// DefaultWidth is the column budget when the caller measured none. A fixed
// number, not "unlimited": redirecting a page into a file or a diff has to
// produce the same bytes on every machine, which a terminal-derived width
// would not.
const DefaultWidth = 80

// MaxWidth caps the wrap even on a very wide terminal. Prose past roughly a
// hundred columns is measurably harder to read, and issue bodies are prose.
const MaxWidth = 100

// MinWidth is the narrowest measurement worth believing. Anything under it is
// treated as no measurement at all.
const MinWidth = 20

// Console is the destination one command renders to, and the complete set of
// facts render is allowed to know about it.
//
// Nothing here is discovered: render never inspects an *os.File, never reads
// $COLUMNS or $PAGER, and never calls ioctl. The caller measures its terminal
// once and states the answer, which is what makes every byte this package
// emits reproducible from a struct literal.
type Console struct {
	// Out is where rendered bytes go.
	Out io.Writer
	// TTY says whether Out is a terminal. It decides truncation, and only
	// truncation: a wrap width is a separate fact.
	TTY bool
	// Width is the measured column budget. Zero, or anything under MinWidth,
	// means unmeasured and falls back to DefaultWidth; anything over MaxWidth
	// is capped to it.
	Width int
	// Pager routes long output through a pager. A nil Pager writes straight
	// through, which is what a non-terminal caller and every test want.
	Pager Pager
}

// Pager runs one render against a paged writer.
//
// An injected collaborator rather than a package-level function var: paging is
// the one thing in this package that starts a process, and a test that has to
// reach into an unexported var to observe it is a test that stops observing
// the moment the wiring changes.
type Pager interface {
	// Page calls render with the writer output should go to. An
	// implementation that cannot start a pager writes straight through
	// rather than failing: not showing an issue because `less` is missing
	// would be the worse bug.
	Page(out io.Writer, render func(io.Writer) error) error
}

// width is the clamped column budget.
func (console Console) width() int {
	if console.Width < MinWidth {
		return DefaultWidth
	}
	return min(console.Width, MaxWidth)
}

// paged runs render against the pager when there is one, and against Out when
// there is not.
func (console Console) paged(render func(io.Writer) error) error {
	if console.Pager == nil {
		return render(console.Out)
	}
	return console.Pager.Page(console.Out, render)
}
