// Package cli is the Cobra presentation over resolve, core, sync, and render.
// It contains no SQL and no business rules: every verb adapts one or more of
// those packages' calls into the agent-facing contract documented in
// docs/agents/issue-tracker.md.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/resolve"
	"github.com/tedkulp/drops/internal/store"
	"github.com/tedkulp/drops/internal/sync"
)

// App carries everything a subcommand needs. It is built lazily so that
// --help, --version and config never open the database.
type App struct {
	ctx     context.Context
	store   *store.Store
	core    *core.Core
	dir     string
	replica model.ReplicaKey

	out  io.Writer
	err  io.Writer
	cwd  string
	json bool

	projectFlag   string
	allProjects   bool
	includeClosed bool
	inbox         bool
	quiet         bool
	noColor       bool

	// warnings redirects the credential scan away from stderr, and is set by
	// exactly one verb: `tui`. Under a full-screen program stderr is
	// invisible, so the default sink would scan every comment you type for an
	// AWS key or a private-key block and silently discard the finding
	// (qy3de.7 §5). It needs no core change — the sink is already a core.New
	// parameter — and the navigator drains the channel after every write.
	warnings chan core.Warning
}

// warnCapacity is the redirected channel's buffer. One write scans at most
// three fields, so a full buffer is unreachable in the one place this is used;
// the non-blocking send is there so a sink can never wedge a commit that has
// already happened.
const warnCapacity = 32

func (a *App) warnSink() core.WarnSink {
	return func(w core.Warning) {
		if a.warnings != nil {
			select {
			case a.warnings <- w:
			default:
			}
			return
		}
		fmt.Fprintf(a.err, "drops: %s matched in %s %s (%s)\n", w.Family, w.Entity.Kind, w.Entity.Key, w.Field)
	}
}

// ExitCodeFor maps a sentinel error to a stable exit code so agents can branch
// on it. Only the sentinels are distinguished; everything else is 1.
//
// 2 is invalid input (including parser misuse), 4 a not-found reference, 5 a
// conflict, 10 a sync lock held by another process. Codes 3, 6, 7, 8 and 9
// belonged to surfaces this map cut: the bd compatibility face and the
// migration subsystem.
func ExitCodeFor(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, model.ErrInvalid):
		return 2
	case errors.Is(err, model.ErrNotFound):
		return 4
	case errors.Is(err, model.ErrConflict):
		return 5
	case errors.Is(err, sync.ErrSyncLocked):
		return 10
	default:
		return 1
	}
}

// Options configures one CLI invocation. Every field has a sensible zero
// value, so Execute() passes Options{} and tests pass explicit values. There
// is deliberately no environment backdoor for cwd or output streams: both
// travel through this struct, never through a test-only env var.
type Options struct {
	DBPath string    // "" -> $DROPS_DB, then ~/.drops/drops.db
	Cwd    string    // "" -> os.Getwd()
	Out    io.Writer // nil -> os.Stdout
	Err    io.Writer // nil -> os.Stderr
}

// invalidArgs wraps a positional-argument rejection so ExitCodeFor maps it to
// 2 (invalid input) rather than the generic 1. It is used at every Args site.
func invalidArgs(format string, args ...any) error {
	return fmt.Errorf("%w: %s", model.ErrInvalid, fmt.Sprintf(format, args...))
}

// exactArgs is cobra.ExactArgs with the rejection wrapped through invalidArgs,
// so an agent can tell "you called it wrong" (2) from "it crashed" (1).
func exactArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return invalidArgs("accepts %d arg(s), received %d", n, len(args))
		}
		return nil
	}
}

// minimumArgs is the same wrapping over a minimum-arity check.
func minimumArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < n {
			return invalidArgs("requires at least %d arg(s), received %d", n, len(args))
		}
		return nil
	}
}

// noArgs is cobra.NoArgs with the rejection wrapped through invalidArgs.
func noArgs() cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return invalidArgs("accepts no args, received %d", len(args))
		}
		return nil
	}
}

// unknownSubcommand rejects any positional reaching a command that only holds
// subcommands, because the only thing a positional can be there is a mistyped
// verb.
//
// Cobra's default is the worst possible answer: a group with no Run prints its
// help and exits 0, so `drops comment edit <id> "..."` reports success having
// written nothing. Root is no better — its legacyArgs rejection is a bare
// error, which lands on exit 1 (crashed) rather than 2 (called it wrong).
func unknownSubcommand() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return invalidArgs("unknown command %q for %q", args[0], cmd.CommandPath())
	}
}

// newGroupCmd builds a command that carries only subcommands: bare, it prints
// its help; with a positional, it refuses through unknownSubcommand.
func newGroupCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  unknownSubcommand(),
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
}

// maximumArgs is cobra.MaximumNArgs with the rejection wrapped through
// invalidArgs.
func maximumArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > n {
			return invalidArgs("accepts at most %d arg(s), received %d", n, len(args))
		}
		return nil
	}
}

// dashLike reports whether r is a glyph a caller could have typed, or had
// substituted for them, where the ASCII "-" that starts a flag belongs. It is
// Unicode's dash punctuation — which the ASCII hyphen itself is a member of,
// along with the en and em dashes a smart-dash substitution produces — plus
// U+2212 MINUS SIGN, which is mathematical rather than punctuation and is the
// other glyph an editor swaps a hyphen for.
func dashLike(r rune) bool {
	return unicode.Is(unicode.Pd, r) || r == '\u2212'
}

// refuseMangledFlag is the check every command in the tree wears in front of
// its own: a positional whose first character is dash-like is refused, naming
// the dash.
//
// Cobra's parser knows only the ASCII "-", so `drops list ——all-projects` —
// the two hyphens arriving as em dashes, from a smart-dash substitution or a
// paste out of a document or chat — is an ordinary positional. That landed two
// ways, both wrong (39rf5, vy56d): a verb taking none reported an argument
// count, naming the wrong thing entirely, and a verb taking one swallowed the
// mangled flag as data at exit 0, which is `drops search ——all-projects`
// answering with a plausible result set for a search nobody asked for.
//
// The refusal is the whole tree's rather than a list of verbs because the rule
// was never a judgment about what text is plausible: pflag already refuses a
// positional whose first character is an ASCII "-" before Args runs at all —
// `drops comment add <id> "- a bullet"` is "unknown shorthand flag: ' '" — so
// refusing the mangled spellings everywhere makes them behave like the
// spelling they were meant to be, and takes nothing away that was reachable.
//
// "--" is the escape, and it needs no new flag: everything after it is a
// positional the caller asked for literally, so `drops create -- "—-help"`
// still files that title and `drops search -- "——all"` searches for it.
func refuseMangledFlag(cmd *cobra.Command, args []string) error {
	literal := cmd.ArgsLenAtDash()
	for i, arg := range args {
		if literal >= 0 && i >= literal {
			break
		}
		if r, _ := utf8.DecodeRuneInString(arg); dashLike(r) {
			return invalidArgs(
				"%q begins with a dash, so it reads as a mangled flag rather than an argument; write the flag with an ASCII \"-\", or put the argument after \"--\" to take it literally",
				arg)
		}
	}
	return nil
}

// positionalArgs puts refuseMangledFlag in front of one command's own Args.
//
// The dash check runs FIRST because every other answer names the wrong thing:
// "accepts no args, received 1" for a scanning verb, "accepts 1 arg(s),
// received 2" for a mangled flag after a real title, "unknown command" for a
// group. None of them mentions the hyphens the caller's editor ate.
//
// A nil Args means ArbitraryArgs to cobra, and it means the same here: the
// wrapper adds the refusal without tightening what the command accepts.
func positionalArgs(own cobra.PositionalArgs) cobra.PositionalArgs {
	if own == nil {
		own = cobra.ArbitraryArgs
	}
	return func(cmd *cobra.Command, args []string) error {
		if err := refuseMangledFlag(cmd, args); err != nil {
			return err
		}
		return own(cmd, args)
	}
}

// refuseMangledFlagsEverywhere wraps every command in the tree, root and the
// groups included, so the refusal is a property of the tree rather than a list
// of verbs somebody has to remember to extend. A command added later is
// covered on arrival, whatever Args it declares — which is the difference
// between this and 39rf5's opt-in wrapper on three capture verbs.
//
// help and cobra's two completion entry points are left alone. None is drops'
// contract: cobra owns their argument handling, and a completion request for a
// half-typed "--all" is a request for candidates, not an invocation to refuse.
// Cobra registers all three during Execute, after this walk has run, so the
// guard is insurance against a future cobra rather than the thing sparing them
// today — which is why the test for it builds its own tree.
func refuseMangledFlagsEverywhere(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		switch cmd.Name() {
		case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return
		}
		cmd.Args = positionalArgs(cmd.Args)
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// defer. Cleanup is the only place that closes the store, and it runs
// regardless of how the command finished — cobra skips PersistentPostRunE when
// RunE errors, which is exactly when the store would otherwise leak.
func NewRootCmd(opts Options) (*cobra.Command, func()) {
	app := &App{ctx: context.Background(), out: os.Stdout, err: os.Stderr, cwd: opts.Cwd}
	if opts.Out != nil {
		app.out = opts.Out
	}
	if opts.Err != nil {
		app.err = opts.Err
	}

	var (
		dbPath string
		closer func() error
	)
	cleanup := func() {
		if closer != nil {
			closer()
			closer = nil
		}
	}

	var root *cobra.Command
	root = &cobra.Command{
		Use:           "drops",
		Short:         "Cross-project issue tracker for AI coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          unknownSubcommand(),
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd == root {
				return nil
			}
			app.out, app.err = cmd.OutOrStdout(), cmd.ErrOrStderr()

			if cmd.Annotations[annotationNoStore] == "1" {
				return nil
			}

			if dbPath == "" {
				p, err := store.DefaultPath()
				if err != nil {
					return err
				}
				dbPath = p
			}
			st, err := setupApp(app, dbPath)
			if err != nil {
				return err
			}
			closer = st.Close
			return nil
		},
	}

	// Flag errors and positional mismatches must surface as exit 2, not the
	// default 1, so an agent can distinguish a bad invocation from a crash.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", model.ErrInvalid, err)
	})

	f := root.PersistentFlags()
	f.StringVar(&dbPath, "db", opts.DBPath, "database path (default $DROPS_DB or ~/.drops/drops.db)")
	f.BoolVar(&app.json, "json", false, "emit JSON: a bare array for lists, a bare object for single items")
	f.StringVarP(&app.projectFlag, "project", "P", "", "scope to this project slug")
	f.BoolVar(&app.allProjects, "all-projects", false, "query every project instead of the current one")
	f.BoolVarP(&app.includeClosed, "all", "a", false, "include closed issues (matches br's -a)")
	f.BoolVar(&app.inbox, "inbox", false, "scope to the reserved inbox project")
	f.BoolVarP(&app.quiet, "quiet", "q", false, "suppress informational output on stderr")
	f.BoolVar(&app.noColor, "no-color", false, "disable coloured output")
	registerDynamicFlagCompletion(root, "project", completeProjectSlugs(app, true))

	registerIssueCmds(root, app)
	registerMoveCmds(root, app)
	registerDepCmds(root, app)
	registerProjectCmds(root, app)
	registerQueryCmds(root, app)
	registerLabelCmds(root, app)
	registerCommentCmds(root, app)
	registerMemoryCmds(root, app)
	registerDoctorCmd(root, app)
	registerSyncCmds(root, app)
	root.AddCommand(newTUICmd(app))

	// Cobra's --version and the version subcommand print one string, built
	// once, so the two answers cannot drift apart.
	root.Version = VersionLine()
	root.SetVersionTemplate("{{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newVersionCmd())
	root.AddCommand(newCompletionCmd())
	root.AddCommand(newConfigCmd(app, &dbPath))

	// Last, so it reaches every command registered above: the mangled-flag
	// refusal is the tree's, not a wrapper each Args site has to opt into.
	refuseMangledFlagsEverywhere(root)

	return root, cleanup
}

// setupApp opens the store, loads the replica sidecar, and builds Core. On
// success the returned store is the caller's to Close; on error nothing is
// left open.
func setupApp(app *App, dbPath string) (*store.Store, error) {
	st, err := store.Open(app.ctx, dbPath)
	if err != nil {
		return nil, err
	}
	app.store = st
	app.dir = filepath.Dir(st.Path())

	sidecar, err := sync.LoadSidecar(app.dir)
	if err != nil {
		st.Close()
		return nil, err
	}
	if sidecar != nil {
		app.replica = sidecar.Replica
	}
	app.core = core.New(st, app.replica, nil, nil, app.warnSink())

	if err := app.core.Bootstrap(app.ctx); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// ensureReplica mints the sidecar on the first local write to a store that had
// none. Reads and imports never reach here, so a merely-read store stays
// sidecarless as the contract requires.
func (a *App) ensureReplica() error {
	if a.replica != "" {
		return nil
	}
	sidecar, err := sync.MintSidecar(a.dir)
	if err != nil {
		return err
	}
	a.replica = sidecar.Replica
	a.core = core.New(a.store, a.replica, nil, nil, a.warnSink())
	return nil
}

// routing is one resolution and the inputs it was decided from. Every caller
// but one wants the answer alone; `config show` reports the evidence too — the
// Git root and origin the ladder compared, and the rung that answered — which
// is the difference between "no project resolves here" and a diagnosis.
type routing struct {
	Request resolve.Request
	Result  resolve.Result
}

// gitFacts describes the repository containing the working directory, from the
// canonical form of that directory. It is the one place Git is asked, so the
// resolver and the verb that reports its evidence cannot disagree about what
// this directory is.
func (a *App) gitFacts() (string, gitx.Facts, error) {
	cwd, err := canonicalPath(a.cwd)
	if err != nil {
		return "", gitx.Facts{}, err
	}
	facts, err := (gitx.Client{}).Inspect(a.ctx, cwd)
	if err != nil {
		return "", gitx.Facts{}, err
	}
	return cwd, facts, nil
}

// resolve runs the pure resolver over everything it needs, already
// canonicalized: the working directory, the repository facts for it, and the
// store's projects, bindings and relevant locators.
func (a *App) resolve(intent resolve.Intent) (routing, error) {
	cwd, facts, err := a.gitFacts()
	if err != nil {
		return routing{}, err
	}
	projects, err := a.core.Projects(a.ctx)
	if err != nil {
		return routing{}, err
	}
	bindings, err := a.core.WorkspaceBindings(a.ctx)
	if err != nil {
		return routing{}, err
	}
	locators := []model.RepositoryLocator(nil)
	if value := resolve.NormalizeLocator(facts.RemoteURL); value != "" {
		locators, err = a.core.ProjectsByLocator(a.ctx, value)
		if err != nil {
			return routing{}, err
		}
	}
	request := resolve.Request{
		Cwd:         cwd,
		ProjectFlag: a.projectFlag,
		EnvProject:  os.Getenv("DROPS_PROJECT"),
		Intent:      intent,
		Git:         facts,
		Projects:    projects,
		Bindings:    bindings,
		Locators:    locators,
	}
	result, err := resolve.Resolve(request)
	return routing{Request: request, Result: result}, err
}

// scopedProject resolves the ambient project and, for writes, enacts any
// proposal the resolver produced: registering an unbound repository, learning
// a workspace binding, or falling back to inbox. Reads never create or bind.
func (a *App) scopedProject(forWrite bool) (model.Project, bool, error) {
	p, found, _, err := a.scopedProjectRouting(forWrite)
	return p, found, err
}

// scopedProjectRouting is scopedProject plus the resolution behind it, for the
// one verb whose job is to explain the answer rather than act on it.
func (a *App) scopedProjectRouting(forWrite bool) (model.Project, bool, routing, error) {
	if a.inbox {
		p, err := a.core.Project(a.ctx, model.InboxProjectKey)
		return p, err == nil, routing{}, err
	}
	intent := resolve.Read
	if forWrite {
		intent = resolve.Write
	}
	routed, err := a.resolve(intent)
	if err != nil {
		return model.Project{}, false, routed, err
	}
	res := routed.Result
	switch res.Outcome {
	case resolve.Found:
		if forWrite && res.Register.BindingPath != "" {
			if err := a.core.BindWorkspace(a.ctx, model.WorkspaceBinding{Path: res.Register.BindingPath, ProjectKey: res.Project}); err != nil {
				return model.Project{}, false, routed, err
			}
		}
		p, err := a.core.Project(a.ctx, res.Project)
		return p, err == nil, routed, err
	case resolve.Unregistered:
		if !forWrite {
			if !a.quiet {
				fmt.Fprintln(a.err, "drops: no project resolves here; pass -P <slug> to scope explicitly")
			}
			return model.Project{}, false, routed, nil
		}
		if res.Register.CreateProject {
			p, err := a.core.RegisterProject(a.ctx, core.Registration{
				Slug:        res.Register.Slug,
				Locator:     res.Register.Locator,
				BindingPath: res.Register.BindingPath,
			})
			return p, err == nil, routed, err
		}
		return model.Project{}, false, routed, nil
	case resolve.Ambiguous:
		return model.Project{}, false, routed, ambiguityError(res.Ambiguity)
	default:
		return model.Project{}, false, routed, fmt.Errorf("resolve: unexpected outcome %d", res.Outcome)
	}
}

func ambiguityError(a resolve.Ambiguity) error {
	switch a.Kind {
	case resolve.AmbiguousLocator:
		return fmt.Errorf("%w: repository %q is claimed by %d projects; bind or pass -P to choose",
			model.ErrConflict, a.Locator, len(a.Projects))
	case resolve.AmbiguousSlug:
		return fmt.Errorf("%w: every candidate slug %v for %q is already taken; register the project explicitly",
			model.ErrConflict, a.Slugs, a.Locator)
	default:
		return fmt.Errorf("%w: ambiguous project", model.ErrConflict)
	}
}

// Execute runs the CLI and terminates the process with a mapped exit code.
func Execute() {
	root, cleanup := NewRootCmd(Options{})
	defer cleanup()
	if err := root.Execute(); err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "drops:", err)
		os.Exit(ExitCodeFor(err))
	}
}

// RunForTest executes the CLI against an explicit database and working
// directory, capturing both streams. Everything travels through Options, so no
// process-global state is mutated and tests may run in parallel.
func RunForTest(args []string, dbPath, cwd string) (string, string, error) {
	var out, errBuf bytes.Buffer
	if args == nil {
		args = []string{}
	}
	root, cleanup := NewRootCmd(Options{
		DBPath: dbPath, Cwd: cwd, Out: &out, Err: &errBuf,
	})
	defer cleanup()

	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err := root.Execute()
	return out.String(), errBuf.String(), err
}

// annotationNoStore marks a command that must work before the database exists.
const annotationNoStore = "no-store"
