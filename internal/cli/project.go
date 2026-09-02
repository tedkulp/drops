package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/resolve"
)

func registerProjectCmds(root *cobra.Command, app *App) {
	project := &cobra.Command{Use: "project", Short: "Manage projects, the routing dimension of the store"}
	project.AddCommand(
		newProjectAddCmd(app),
		newProjectArchiveCmd(app),
		newProjectListCmd(app),
		newProjectRenameCmd(app),
	)
	root.AddCommand(project)
}

func newProjectAddCmd(app *App) *cobra.Command {
	var (
		slug, remote, repoPath string
	)
	cmd := &cobra.Command{
		Use:   "add --slug <slug>",
		Short: "Register a project explicitly",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			if slug == "" {
				return invalidArgs("--slug <slug> is required")
			}
			reg := core.Registration{Slug: slug}
			if remote != "" {
				reg.Locator = resolve.NormalizeLocator(remote)
			}
			if repoPath != "" {
				canon, err := canonicalPath(repoPath)
				if err != nil {
					return err
				}
				reg.BindingPath = canon
			}
			p, err := app.core.RegisterProject(app.ctx, reg)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, p)
			}
			fmt.Fprintln(app.out, "added", p.Slug)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&slug, "slug", "", "project slug (required)")
	f.StringVar(&remote, "remote", "", "git remote url; normalized before storage")
	f.StringVar(&repoPath, "repo-path", "", "canonical repository root")
	return cmd
}

func newProjectArchiveCmd(app *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "archive <slug>",
		Short: "Archive a project, hiding it from listings and --all-projects",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			p, err := app.core.ProjectBySlug(app.ctx, args[0])
			if err != nil {
				return err
			}
			if !force {
				if isTerminal(os.Stdin) {
					fmt.Fprintf(app.err, "archive %s? [y/N] ", args[0])
					var answer string
					fmt.Fscanln(os.Stdin, &answer)
					if answer != "y" && answer != "Y" {
						return invalidArgs("archive refused")
					}
				} else {
					return invalidArgs("archiving is destructive; pass --force to confirm from a non-interactive session")
				}
			}
			archived, err := app.core.ArchiveProject(app.ctx, p.Key)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, archived)
			}
			fmt.Fprintln(app.out, "archived", archived.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt")
	return cmd
}

func newProjectListCmd(app *App) *cobra.Command {
	var archivedFlag bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			projects, err := app.core.Projects(app.ctx)
			if err != nil {
				return err
			}
			kept := projects[:0]
			for _, p := range projects {
				if p.ArchivedAt != nil && !archivedFlag {
					continue
				}
				kept = append(kept, p)
			}
			projects = kept
			if app.json {
				return render.EmitMany(app.out, projects)
			}
			for _, p := range projects {
				fmt.Fprintln(app.out, p.Slug)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&archivedFlag, "archived", false, "include archived projects")
	return cmd
}

func newProjectRenameCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old-slug> <new-slug>",
		Short: "Rename a project slug",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			p, err := app.core.ProjectBySlug(app.ctx, args[0])
			if err != nil {
				return err
			}
			renamed, err := app.core.RenameProject(app.ctx, p.Key, args[1])
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, renamed)
			}
			fmt.Fprintln(app.out, "renamed", renamed.Slug)
			return nil
		},
	}
}
