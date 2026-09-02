package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerMemoryCmds(root *cobra.Command, app *App) {
	root.AddCommand(
		newRememberCmd(app),
		newMemoriesCmd(app),
		newMemoryCmd(app),
		newForgetCmd(app),
		newSupersedeCmd(app),
	)
}

func newRememberCmd(app *App) *cobra.Command {
	var (
		title, source string
		global        bool
	)
	cmd := &cobra.Command{
		Use:   "remember <text>",
		Short: "Store a memory",
		Long: "Store a memory.\n\n" +
			"Every memory belongs to one project. Writes use the ordinary project\n" +
			"resolution; an unresolved directory files under `inbox`, and --global\n" +
			"assigns the reserved `global` project.",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			if global && app.projectFlag != "" {
				return invalidArgs("-P %s scopes this memory to a project and --global forces it cross-project; pass one", app.projectFlag)
			}
			project, err := app.memoryScope(global)
			if err != nil {
				return err
			}
			m, err := app.core.CreateMemory(app.ctx, core.CreateMemory{
				Project:    project,
				Title:      title,
				Body:       args[0],
				Provenance: source,
			})
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, m)
			}
			fmt.Fprintln(app.out, m.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "explicit title")
	f.StringVar(&source, "source", "", "free-text provenance, e.g. a skill name")
	f.BoolVar(&global, "global", false, "assign the reserved global project")
	return cmd
}

func newMemoriesCmd(app *App) *cobra.Command {
	var (
		all, deleted bool
		limit        int
	)
	cmd := &cobra.Command{
		Use:   "memories [query]",
		Short: "List or search memories across every project",
		Long: "List memories, or search them when a query is given.\n\n" +
			"Reads span EVERY project by default, the inverse of the issue verbs: a memory\n" +
			"bound to one repo is still findable from another. -P <slug> narrows to one.",
		Args: maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := app.memoryReadProject()
			if err != nil {
				return err
			}
			filter := core.MemoryFilter{
				Project:           project,
				IncludeSuperseded: all,
				IncludeTombstoned: all || deleted,
				Limit:             limit,
			}
			var ms []model.Memory
			if len(args) == 0 {
				ms, err = app.core.Memories(app.ctx, filter)
			} else {
				ms, err = app.core.SearchMemories(app.ctx, args[0], filter)
			}
			if err != nil {
				return err
			}
			if deleted {
				kept := ms[:0]
				for _, m := range ms {
					if m.Tombstone == model.Tombstoned {
						kept = append(kept, m)
					}
				}
				ms = kept
			}
			return emitMemories(app, ms)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&all, "all", "a", false, "reveal tombstoned and superseded memories")
	f.BoolVar(&deleted, "deleted", false, "reveal ONLY tombstoned memories")
	f.IntVar(&limit, "limit", 0, "maximum results (0 = unlimited)")
	return cmd
}

func newMemoryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Read or edit a memory by exact id",
		Args:  noArgs(),
	}
	cmd.AddCommand(
		newMemoryShowCmd(app),
		newMemoryEditCmd(app),
	)
	return cmd
}

func newMemoryShowCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one memory in full, retired or tombstoned included",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := app.core.Memory(app.ctx, model.ID(args[0]))
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, m)
			}
			printMemoryFull(app, m)
			return nil
		},
	}
}

func newMemoryEditCmd(app *App) *cobra.Command {
	var (
		title, body, source string
		global              bool
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a memory's title, body, project, or provenance",
		Long:  "Edit a memory's notebook content; only the flags you pass are changed.",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			f := cmd.Flags()
			var edit core.MemoryEdit
			if f.Changed("title") {
				edit.Title = &title
			}
			if f.Changed("body") {
				edit.Body = &body
			}
			if f.Changed("source") {
				var p *string
				if source != "" {
					p = &source
				}
				edit.Provenance = &p
			}
			if global && app.projectFlag != "" {
				return invalidArgs("-P %s moves this memory into a project and --global makes it cross-project; pass one", app.projectFlag)
			}
			switch {
			case global:
				key := model.GlobalProjectKey
				edit.Project = &key
			case app.projectFlag != "":
				p, err := app.core.ProjectBySlug(app.ctx, app.projectFlag)
				if err != nil {
					return fmt.Errorf("unknown project %q: %w", app.projectFlag, err)
				}
				edit.Project = &p.Key
			}
			m, err := app.core.EditMemory(app.ctx, model.ID(args[0]), edit)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, m)
			}
			fmt.Fprintln(app.out, "updated", m.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "new title")
	f.StringVar(&body, "body", "", "new body")
	f.StringVar(&source, "source", "", "new free-text provenance")
	f.BoolVar(&global, "global", false, "make the memory cross-project")
	return cmd
}

func newForgetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <id>...",
		Short: "Tombstone a memory",
		Long: "Tombstone a memory. The row is never deleted: something may reference it,\n" +
			"and --deleted brings it back.",
		Args: minimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			for _, raw := range args {
				id := model.ID(raw)
				if _, err := app.core.SetMemoryTombstone(app.ctx, id, model.Tombstoned); err != nil {
					return err
				}
				if !app.json {
					fmt.Fprintln(app.out, "forgot", id)
				}
			}
			return nil
		},
	}
}

func newSupersedeCmd(app *App) *cobra.Command {
	var (
		title, source string
		global        bool
	)
	cmd := &cobra.Command{
		Use:   "supersede <id> <text>",
		Short: "Replace a memory with a new one and link the old to it",
		Long: "Store a replacement for <id> and point the old memory at it. The old memory\n" +
			"stays in the store, hidden from the default read paths and reachable under\n" +
			"--all. The replacement inherits the old memory's project.",
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			if global && app.projectFlag != "" {
				return invalidArgs("-P %s scopes this memory to a project and --global forces it cross-project; pass one", app.projectFlag)
			}
			oldID := model.ID(args[0])
			old, err := app.core.Memory(app.ctx, oldID)
			if err != nil {
				return err
			}
			project := old.ProjectKey
			if global {
				project = model.GlobalProjectKey
			} else if app.projectFlag != "" {
				p, err := app.core.ProjectBySlug(app.ctx, app.projectFlag)
				if err != nil {
					return fmt.Errorf("unknown project %q: %w", app.projectFlag, err)
				}
				project = p.Key
			}
			m, err := app.core.SupersedeMemory(app.ctx, oldID, core.CreateMemory{
				Project:    project,
				Title:      title,
				Body:       args[1],
				Provenance: source,
			})
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, m)
			}
			fmt.Fprintln(app.out, m.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "explicit title")
	f.StringVar(&source, "source", "", "free-text provenance, e.g. a skill name")
	f.BoolVar(&global, "global", false, "assign the reserved global project")
	return cmd
}

// memoryScope resolves where a memory write lands: --global names the reserved
// global project, -P a named project, and neither uses the ordinary write
// resolution (which may learn a new project, else falls back to inbox).
func (a *App) memoryScope(global bool) (model.ProjectKey, error) {
	switch {
	case global:
		return model.GlobalProjectKey, nil
	case a.projectFlag != "":
		p, err := a.core.ProjectBySlug(a.ctx, a.projectFlag)
		if err != nil {
			return "", fmt.Errorf("unknown project %q: %w", a.projectFlag, err)
		}
		return p.Key, nil
	default:
		scoped, _, err := a.scopedProject(true)
		return scoped.Key, err
	}
}

// memoryReadProject reports the project a memory read is narrowed to, nil for
// every project (the default, the inverse of the issue verbs).
func (a *App) memoryReadProject() (*model.ProjectKey, error) {
	if a.projectFlag == "" {
		return nil, nil
	}
	p, err := a.core.ProjectBySlug(a.ctx, a.projectFlag)
	if err != nil {
		return nil, fmt.Errorf("unknown project %q: %w", a.projectFlag, err)
	}
	return &p.Key, nil
}

func emitMemories(app *App, ms []model.Memory) error {
	if app.json {
		return render.EmitMany(app.out, ms)
	}
	for _, m := range ms {
		printMemory(app, m)
	}
	return nil
}

func printMemory(app *App, m model.Memory) {
	mark := "○"
	if m.Tombstone == model.Tombstoned {
		mark = "⊘"
	}
	line := fmt.Sprintf("%s %s  %s", mark, m.ID, m.Title)
	if state := memoryState(m); state != "" {
		line += " · " + state
	}
	fmt.Fprintln(app.out, line)
}
func memoryState(m model.Memory) string {
	switch {
	case m.Tombstone == model.Tombstoned:
		return "tombstoned"
	case m.SupersededBy != nil:
		return "superseded by " + string(*m.SupersededBy)
	default:
		return ""
	}
}

func printMemoryFull(app *App, m model.Memory) {
	line := fmt.Sprintf("%s · %s", m.ID, m.Title)
	if m.Provenance != nil && *m.Provenance != "" {
		line += " · from " + *m.Provenance
	}
	if state := memoryState(m); state != "" {
		line += " · " + state
	}
	fmt.Fprintln(app.out, line)
	fmt.Fprintln(app.out, m.Body)
}
