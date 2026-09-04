package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/resolve"
	"github.com/tedkulp/drops/internal/tui"
)

func newTUICmd(app *App) *cobra.Command {
	var author string
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
			"Press ? for the keymap.",
		Args: noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := app.requireProject()
			if err != nil {
				return err
			}
			return tui.New(app.core, project, resolveAuthor(author)).Run(app.ctx)
		},
	}
	cmd.Flags().StringVar(&author, "author", "",
		"who a comment written from the navigator is attributed to (default: git user.name, else the OS username)")
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
	resolved, err := a.resolve(resolve.Read)
	if err != nil {
		return model.Project{}, err
	}
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
