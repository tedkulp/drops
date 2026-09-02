package cli

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerCommentCmds(root *cobra.Command, app *App) {
	comment := newGroupCmd("comment", "Read and write issue comments")
	comment.AddCommand(
		newCommentAddCmd(app),
		newCommentListCmd(app),
		newCommentRmCmd(app),
	)
	root.AddCommand(comment)
}

// resolveAuthor works out who is commenting: --author if given, else the
// global git user.name, else the OS username. Never an error.
func resolveAuthor(flag string) string {
	if flag != "" {
		return flag
	}
	if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}

func newCommentAddCmd(app *App) *cobra.Command {
	var author string
	cmd := &cobra.Command{
		Use:   "add <id> <body>",
		Short: "Append a comment to an issue",
		Long: "Append a comment to an issue and print the new comment's id.\n\n" +
			"The body is a positional argument, so a long one comes from the shell:\n" +
			"`drops comment add <id> \"$(cat note.md)\"`. There is no --body-file and\n" +
			"no stdin form here.\n\n" +
			"The author defaults to the global git user.name, else the OS username.\n" +
			"An agent commenting on its own behalf should pass --author.",
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			c, err := app.core.AddComment(app.ctx, model.ID(args[0]), resolveAuthor(author), args[1])
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, c)
			}
			fmt.Fprintln(app.out, c.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&author, "author", "",
		"who is commenting (default: global git user.name, else the OS username)")
	return cmd
}

func newCommentListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list <id>",
		Short: "Print an issue's comment thread, oldest first",
		Long: "Print an issue's whole comment thread, oldest first, in the same\n" +
			"rendering `show` uses.\n\n" +
			"--json emits a bare array, [] when the issue has no comments.",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cs, err := app.core.IssueComments(app.ctx, model.ID(args[0]))
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitMany(app.out, cs)
			}
			console := newConsole(app.out, app.err)
			return renderComments(console, cs)
		},
	}
}

func newCommentRmCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <comment-id>",
		Short: "Remove one comment",
		Long: "Remove one comment. The comment id carries its issue, so no issue\n" +
			"argument is needed; `comment list` and `show` both print it.\n\n" +
			"This is a hard delete, for undoing a mistyped comment. There is\n" +
			"deliberately no `comment edit`.",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			if err := app.core.DeleteComment(app.ctx, model.CommentID(args[0])); err != nil {
				return err
			}
			if !app.json {
				fmt.Fprintf(app.out, "removed comment %s\n", args[0])
			}
			return nil
		},
	}
}
