package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryConfig controls repository initialization. Empty identity fields
// use a deterministic local identity rather than inherited global config.
type RepositoryConfig struct {
	Dir       string
	UserName  string
	UserEmail string
}

// CommitResult describes an attempted mirror commit.
type CommitResult struct {
	SHA     string
	Created bool
	Forced  bool
}

// FetchResult identifies the fetched branch tip without changing the worktree.
type FetchResult struct {
	SHA string
}

// Ensure initializes and hardens a non-bare repository. It is idempotent.
func (c Client) Ensure(ctx context.Context, cfg RepositoryConfig) error {
	if cfg.Dir == "" {
		return errors.New("ensure Git repository: empty directory")
	}
	if cfg.UserName == "" {
		cfg.UserName = "drops"
	}
	if cfg.UserEmail == "" {
		cfg.UserEmail = "drops@localhost"
	}

	gitDir := filepath.Join(cfg.Dir, ".git")
	if _, err := os.Stat(gitDir); errors.Is(err, os.ErrNotExist) {
		if _, err := c.run(ctx, cfg.Dir, c.localTimeout(), false, "init", "-q", "-b", "main"); err != nil {
			return fmt.Errorf("initialize Git repository: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect Git repository: %w", err)
	}
	if err := os.Chmod(gitDir, 0o700); err != nil {
		return fmt.Errorf("secure Git repository: %w", err)
	}

	hooksDir := filepath.Join(gitDir, "drops-hooks")
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		return fmt.Errorf("create empty Git hooks directory: %w", err)
	}
	settings := [...][2]string{
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
		{"user.name", cfg.UserName},
		{"user.email", cfg.UserEmail},
		{"core.hooksPath", hooksDir},
		{"gc.autoDetach", "true"},
	}
	for _, setting := range settings {
		if _, err := c.run(ctx, cfg.Dir, c.localTimeout(), false,
			"config", "--local", setting[0], setting[1]); err != nil {
			return fmt.Errorf("configure Git %s: %w", setting[0], err)
		}
	}
	return nil
}

// Commit stages exactly paths and creates a commit only when their staged
// content changed. Other files in the worktree are never staged.
func (c Client) Commit(
	ctx context.Context,
	dir, message string,
	paths []string,
) (CommitResult, error) {
	if len(paths) == 0 {
		return CommitResult{}, errors.New("commit Git mirror: no paths")
	}
	forced, err := c.stage(ctx, dir, paths)
	if err != nil {
		return CommitResult{}, fmt.Errorf("stage Git mirror: %w", err)
	}

	if _, err := c.run(ctx, dir, c.localTimeout(), true, "diff", "--cached", "--quiet", "--"); err == nil {
		return CommitResult{}, nil
	} else if !isExitCode(err, 1) {
		return CommitResult{}, fmt.Errorf("inspect staged Git mirror: %w", err)
	}

	if _, err := c.run(ctx, dir, c.localTimeout(), false,
		"commit", "--no-gpg-sign", "-q", "-m", message); err != nil {
		return CommitResult{}, fmt.Errorf("commit Git mirror: %w", err)
	}
	sha, err := c.run(ctx, dir, c.localTimeout(), true, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return CommitResult{Created: true, Forced: forced}, fmt.Errorf("read Git commit: %w", err)
	}
	return CommitResult{SHA: sha, Created: true, Forced: forced}, nil
}

// MergeCommit records already-resolved files with the current HEAD and
// otherParent as parents. Git never merges file content; sync resolves the
// logical records before calling this method.
func (c Client) MergeCommit(
	ctx context.Context,
	dir, message string,
	paths []string,
	otherParent string,
) (CommitResult, error) {
	if len(paths) == 0 {
		return CommitResult{}, errors.New("commit resolved Git mirror: no paths")
	}
	current, err := c.run(ctx, dir, c.localTimeout(), true, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return CommitResult{}, fmt.Errorf("read local Git head: %w", err)
	}
	if otherParent == "" || otherParent == current {
		return CommitResult{}, errors.New("commit resolved Git mirror: invalid other parent")
	}

	forced, err := c.stage(ctx, dir, paths)
	if err != nil {
		return CommitResult{}, fmt.Errorf("stage resolved Git mirror: %w", err)
	}

	tree, err := c.run(ctx, dir, c.localTimeout(), false, "write-tree")
	if err != nil {
		return CommitResult{}, fmt.Errorf("write resolved Git tree: %w", err)
	}
	sha, err := c.run(ctx, dir, c.localTimeout(), false,
		"commit-tree", tree, "-p", current, "-p", otherParent, "-m", message)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit resolved Git mirror: %w", err)
	}
	if _, err := c.run(ctx, dir, c.localTimeout(), false,
		"update-ref", "HEAD", sha, current); err != nil {
		return CommitResult{}, fmt.Errorf("advance resolved Git head: %w", err)
	}
	return CommitResult{SHA: sha, Created: true, Forced: forced}, nil
}

// Fetch downloads one branch and returns its tip without updating a local
// branch or the worktree.
func (c Client) Fetch(
	ctx context.Context,
	dir, remote, branch string,
) (FetchResult, error) {
	if remote == "" || branch == "" {
		return FetchResult{}, errors.New("fetch Git mirror: empty remote or branch")
	}
	if _, err := c.run(ctx, dir, c.networkTimeout(), false,
		"fetch", "--quiet", "--no-tags", "--end-of-options", remote, "refs/heads/"+branch); err != nil {
		return FetchResult{}, fmt.Errorf("fetch Git mirror: %w", err)
	}
	sha, err := c.run(ctx, dir, c.localTimeout(), true, "rev-parse", "--verify", "FETCH_HEAD")
	if err != nil {
		return FetchResult{}, fmt.Errorf("read fetched Git head: %w", err)
	}
	return FetchResult{SHA: sha}, nil
}

// ReadFile returns path exactly as stored at a fetched or local commit.
func (c Client) ReadFile(ctx context.Context, dir, commit, path string) ([]byte, error) {
	if commit == "" || path == "" {
		return nil, errors.New("read Git file: empty commit or path")
	}
	out, err := c.runBytes(ctx, dir, c.localTimeout(), true,
		"show", "--no-textconv", "--format=", commit+":"+path)
	if err != nil {
		return nil, fmt.Errorf("read Git file %s at %s: %w", path, commit, err)
	}
	return out, nil
}

// Push updates branch from the current HEAD.
func (c Client) Push(ctx context.Context, dir, remote, branch string) error {
	if remote == "" || branch == "" {
		return errors.New("push Git mirror: empty remote or branch")
	}
	if _, err := c.run(ctx, dir, c.networkTimeout(), false,
		"push", "--quiet", "--end-of-options", remote, "HEAD:refs/heads/"+branch); err != nil {
		return fmt.Errorf("push Git mirror: %w", err)
	}
	return nil
}

func (c Client) stage(ctx context.Context, dir string, paths []string) (bool, error) {
	pathspecs := literalPathspecs(paths)
	addArgs := make([]string, 0, len(pathspecs)+2)
	addArgs = append(addArgs, "add", "--")
	addArgs = append(addArgs, pathspecs...)
	if _, err := c.run(ctx, dir, c.localTimeout(), false, addArgs...); err == nil {
		return false, nil
	} else if !isIgnoredPathError(err) {
		return false, err
	}

	forceArgs := make([]string, 0, len(pathspecs)+3)
	forceArgs = append(forceArgs, "add", "--force", "--")
	forceArgs = append(forceArgs, pathspecs...)
	if _, err := c.run(ctx, dir, c.localTimeout(), false, forceArgs...); err != nil {
		return false, err
	}
	return true, nil
}

func literalPathspecs(paths []string) []string {
	pathspecs := make([]string, len(paths))
	for i, path := range paths {
		pathspecs[i] = ":(literal)" + path
	}
	return pathspecs
}

func isIgnoredPathError(err error) bool {
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	message := strings.ToLower(commandErr.Stderr)
	return strings.Contains(message, "ignored by one of your .gitignore files") ||
		strings.Contains(message, "is ignored by")
}

func isExitCode(err error, code int) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) && commandErr.ExitCode == code && !commandErr.TimedOut
}
