package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/resolve"
	"github.com/tedkulp/drops/internal/tui"
)

func newTUICmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Browse the current project's issues in a two-pane navigator",
		Long: "Open the two-pane issue navigator: the current project's live issues on\n" +
			"the left, one issue in full on the right, and the right pane retargetable\n" +
			"without the list cursor moving.\n\n" +
			"Unlike `list` and `ready`, this verb REFUSES to start where no project\n" +
			"resolves, and exits non-zero. Those verbs can afford a stderr advisory\n" +
			"because it stays on the terminal; under an alt screen it is invisible, so\n" +
			"exiting 0 would be a blank pane with no explanation.\n\n" +
			"`x` writes: close, reopen, comment, priority, claim, release. Text goes\n" +
			"through $VISUAL, then $EDITOR, then vi.\n\n" +
			"Press ? for the keymap.",
		Args: noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := app.requireProject()
			if err != nil {
				return err
			}
			// The navigator writes, so the sidecar is minted before the
			// screen is taken over rather than on the first `x`. That is a
			// documented exception to "minted on the first local write":
			// under an alt screen a failure to mint has nowhere to be
			// reported, which is the same reason this verb refuses to start
			// where no project resolves. It also REPLACES app.core, so it has
			// to run before the pane is handed one.
			if err := app.ensureReplica(); err != nil {
				return err
			}
			// The credential scan goes to a channel the pane drains instead
			// of to stderr, which an alt screen swallows (qy3de.7 §5).
			app.warnings = make(chan core.Warning, warnCapacity)

			// There is deliberately no --author flag. The flag exists on
			// `comment add` so an agent can say who it is, and there is no
			// agent here: an agent cannot drive an alt-screen program, so a
			// comment written from the navigator is written by a human at a
			// keyboard, always. A wrong name is fixed with
			// `git config --global user.name`, which is where resolveAuthor
			// already reads it.
			return tui.New(app.core, project, resolveAuthor(""),
				tui.ResolveEditor(os.Getenv("VISUAL"), os.Getenv("EDITOR")),
				app.warnings).Run(app.ctx)
		},
	}
	return cmd
}

// requireProject resolves the ambient project for a verb that cannot run
// without one.
//
// It is deliberately not scopedProject: that one prints "no project resolves
// here" to stderr and reports found=false, which is right for a read verb that
// then exits 0 with an empty result. Here the refusal has to BE the error, so
// that the one message an operator sees is the one the exit code belongs to
// rather than an advisory followed by a failure.
func (a *App) requireProject() (model.Project, error) {
	if a.inbox {
		return a.core.Project(a.ctx, model.InboxProjectKey)
	}
	routed, err := a.resolve(resolve.Read)
	if err != nil {
		return model.Project{}, err
	}
	resolved := routed.Result
	switch resolved.Outcome {
	case resolve.Found:
		return a.core.Project(a.ctx, resolved.Project)
	case resolve.Ambiguous:
		return model.Project{}, ambiguityError(resolved.Ambiguity)
	default:
		return model.Project{}, fmt.Errorf(
			"%w: no project resolves here; pass -P <slug> to scope explicitly", model.ErrInvalid)
	}
}
