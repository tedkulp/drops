// Package cli issue verbs: create, q, show, update, claim, release, close, reopen.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerIssueCmds(root *cobra.Command, app *App) {
	root.AddCommand(
		newCreateCmd(app),
		newQuickCmd(app),
		newShowCmd(app),
		newUpdateCmd(app),
		newClaimCmd(app),
		newReleaseCmd(app),
		newCloseCmd(app),
		newReopenCmd(app),
	)
}

func newCreateCmd(app *App) *cobra.Command {
	var (
		issueType   string
		priority    int
		description string
		assignee    string
		parent      string
		labels      []string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create an issue in the current project",
		Long: "Create an issue in the project resolved from the working directory.\n" +
			"A nested repository resolves to its own project, not its parent.\n" +
			"With --parent, the issue goes in its parent's project instead and\n" +
			"the working directory is not consulted.",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			var project model.ProjectKey
			var parentID *model.ID
			if parent != "" {
				p, err := app.core.Issue(app.ctx, model.ID(parent))
				if err != nil {
					return err
				}
				if err := parentScopeConflict(app, p.ProjectKey); err != nil {
					return err
				}
				project = p.ProjectKey
				parsed := model.ID(parent)
				parentID = &parsed
			} else {
				scoped, _, err := app.scopedProject(true)
				if err != nil {
					return err
				}
				project = scoped.Key
			}
			in := core.CreateIssue{
				Project:     project,
				Parent:      parentID,
				Title:       args[0],
				Description: description,
				Type:        model.IssueType(issueType),
				Priority:    priority,
				Labels:      labels,
			}
			if assignee != "" {
				in.Assignee = &assignee
			}
			issue, err := app.core.CreateIssue(app.ctx, in)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, issue)
			}
			fmt.Fprintln(app.out, issue.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&issueType, "type", "t", "task", "issue type: task|bug|feature|epic|chore|research|decision")
	f.IntVarP(&priority, "priority", "p", 2, "priority 0 (critical) to 4 (backlog)")
	f.StringVarP(&description, "description", "d", "", "description body")
	f.StringVarP(&assignee, "assignee", "A", "", "assignee")
	f.StringVar(&parent, "parent", "", "parent issue id; the new issue takes a .N child id")
	f.StringSliceVarP(&labels, "label", "l", nil, "labels (repeatable or comma-separated)")
	registerIssueTypeCompletion(cmd)
	registerPriorityCompletion(cmd)
	registerDynamicFlagCompletion(cmd, "label", completeLabels(app))
	registerDynamicFlagCompletion(cmd, "parent", completeIssueIDsForFlag(app))
	return cmd
}

// parentScopeConflict refuses an explicit -P or --inbox that disagrees with the
// project --parent files the child into. The cwd is ambient and carries no
// intention, which is why --parent silently overrides it; a typed scope that
// contradicts it is an error.
func parentScopeConflict(app *App, parentProject model.ProjectKey) error {
	if app.projectFlag == "" && !app.inbox {
		return nil
	}
	if app.inbox {
		if parentProject == model.InboxProjectKey {
			return nil
		}
		return fmt.Errorf(
			"%w: --parent puts this issue in project %q, but --inbox names %q; a child goes where its parent is",
			model.ErrInvalid, projectSlug(app, parentProject), model.InboxProjectSlug)
	}
	if projectSlug(app, parentProject) == app.projectFlag {
		return nil
	}
	return fmt.Errorf(
		"%w: --parent puts this issue in project %q, but -P names %q; a child goes where its parent is",
		model.ErrInvalid, projectSlug(app, parentProject), app.projectFlag)
}

func projectSlug(app *App, key model.ProjectKey) string {
	p, err := app.core.Project(app.ctx, key)
	if err != nil {
		return string(key)
	}
	return p.Slug
}

func newQuickCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "q <title>",
		Short: "Quick capture: create an issue and print only its id",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			scoped, _, err := app.scopedProject(true)
			if err != nil {
				return err
			}
			issue, err := app.core.CreateIssue(app.ctx, core.CreateIssue{
				Project:  scoped.Key,
				Title:    args[0],
				Type:     model.TypeTask,
				Priority: 2,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(app.out, issue.ID)
			return nil
		},
	}
}

func newShowCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>...",
		Short: "Show one or more issues in full",
		Long: "Show one or more issues in full: identity, body, every relation, and\n" +
			"the whole comment thread.\n\n" +
			"--json emits the same data as one object per issue, and its shape does\n" +
			"not vary: parent is null when there is none; blockers, blocking,\n" +
			"children, discovered_from, discovered, related, and comments are [].",
		Args:              minimumArgs(1),
		ValidArgsFunction: completeIssueIDs(app, -1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages := make([]render.Page, 0, len(args))
			jsons := make([]showIssueJSON, 0, len(args))
			for _, raw := range args {
				issueView, err := app.core.ViewIssue(app.ctx, model.ID(raw))
				if err != nil {
					return err
				}
				page, j := assembleView(issueView)
				pages = append(pages, page)
				jsons = append(jsons, j)
			}
			if app.json {
				if len(jsons) == 1 {
					return render.EmitOne(app.out, jsons[0])
				}
				return render.EmitMany(app.out, jsons)
			}
			return renderPages(newConsole(app.out, app.err), pages)
		},
	}
}

func newUpdateCmd(app *App) *cobra.Command {
	var (
		title, description, assignee, issueType string
		priority                                int
		status                                  string
	)
	cmd := &cobra.Command{
		Use:               "update <id>",
		Short:             "Update an issue; only the flags you pass are changed",
		Args:              exactArgs(1),
		ValidArgsFunction: completeIssueIDs(app, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			id := model.ID(args[0])
			f := cmd.Flags()

			var last model.Issue
			if f.Changed("status") {
				issue, err := app.core.SetIssueStatus(app.ctx, id, model.Status(status), "")
				if err != nil {
					return err
				}
				last = issue
			}
			var edit core.IssueEdit
			if f.Changed("title") {
				edit.Title = &title
			}
			if f.Changed("description") {
				edit.Description = &description
			}
			if f.Changed("type") {
				t := model.IssueType(issueType)
				edit.Type = &t
			}
			if f.Changed("priority") {
				edit.Priority = &priority
			}
			if f.Changed("assignee") {
				p := &assignee
				edit.Assignee = &p
			}
			if edit.Title != nil || edit.Description != nil || edit.Type != nil || edit.Priority != nil || edit.Assignee != nil {
				issue, err := app.core.EditIssue(app.ctx, id, edit)
				if err != nil {
					return err
				}
				last = issue
			}
			if app.json {
				return render.EmitOne(app.out, last)
			}
			fmt.Fprintln(app.out, "updated", id)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "new title")
	f.StringVarP(&description, "description", "d", "", "new description")
	f.StringVar(&status, "status", "", "new status: open|in_progress|closed")
	f.StringVarP(&issueType, "type", "t", "", "new issue type")
	f.StringVarP(&assignee, "assignee", "A", "", "new assignee")
	f.IntVarP(&priority, "priority", "p", 2, "new priority 0-4")
	registerStatusCompletion(cmd, false)
	registerIssueTypeCompletion(cmd)
	registerPriorityCompletion(cmd)
	return cmd
}

func newClaimCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:               "claim <id> <who>",
		Short:             "Claim an unassigned issue",
		Args:              exactArgs(2),
		ValidArgsFunction: completeIssueIDs(app, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			issue, err := app.core.ClaimIssue(app.ctx, model.ID(args[0]), args[1])
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, issue)
			}
			fmt.Fprintln(app.out, "claimed", issue.ID, "by", args[1])
			return nil
		},
	}
}

func newReleaseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:               "release <id>",
		Short:             "Release an issue's claim",
		Args:              exactArgs(1),
		ValidArgsFunction: completeIssueIDs(app, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			issue, err := app.core.ReleaseIssue(app.ctx, model.ID(args[0]))
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, issue)
			}
			fmt.Fprintln(app.out, "released", issue.ID)
			return nil
		},
	}
}

func newCloseCmd(app *App) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:               "close <id>",
		Short:             "Close an issue",
		Args:              exactArgs(1),
		ValidArgsFunction: completeIssueIDs(app, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			issue, err := app.core.SetIssueStatus(app.ctx, model.ID(args[0]), model.StatusClosed, reason)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, issue)
			}
			fmt.Fprintln(app.out, "closed", issue.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why it was closed")
	return cmd
}

func newReopenCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:               "reopen <id>",
		Short:             "Reopen a closed issue",
		Args:              exactArgs(1),
		ValidArgsFunction: completeClosedIssueIDs(app, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			issue, err := app.core.SetIssueStatus(app.ctx, model.ID(args[0]), model.StatusOpen, "")
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, issue)
			}
			fmt.Fprintln(app.out, "reopened", issue.ID)
			return nil
		},
	}
}
