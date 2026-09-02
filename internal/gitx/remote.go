package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Head returns the SHA of HEAD, or "" when the branch is unborn. A repository
// with no commits yet is a normal state for a store that has never synced, so it
// is not an error.
func (c Client) Head(ctx context.Context, dir string) (string, error) {
	sha, err := c.run(ctx, dir, c.localTimeout(), true, "rev-parse", "--verify", "HEAD")
	if err == nil {
		return sha, nil
	}
	if isUnbornHead(err) {
		return "", nil
	}
	return "", fmt.Errorf("read Git head: %w", err)
}

func isUnbornHead(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) &&
		commandErr.ExitCode >= 0 &&
		!commandErr.TimedOut &&
		(strings.Contains(commandErr.Stderr, "Needed a single revision") ||
			strings.Contains(commandErr.Stderr, "unknown revision"))
}

// SetRemoteURL points remote name at url, adding it when absent and replacing it
// when it already exists. Idempotent: calling again with the same url is a no-op.
func (c Client) SetRemoteURL(ctx context.Context, dir, name, url string) error {
	if name == "" || url == "" {
		return errors.New("set Git remote: empty name or url")
	}
	current, err := c.run(ctx, dir, c.localTimeout(), true, "remote", "get-url", name)
	if err == nil {
		if current == url {
			return nil
		}
		if _, err := c.run(ctx, dir, c.localTimeout(), false, "remote", "set-url", name, url); err != nil {
			return fmt.Errorf("set Git remote %s: %w", name, err)
		}
		return nil
	}
	if !isMissingRemote(err) {
		return fmt.Errorf("read Git remote %s: %w", name, err)
	}
	if _, err := c.run(ctx, dir, c.localTimeout(), false, "remote", "add", name, url); err != nil {
		return fmt.Errorf("add Git remote %s: %w", name, err)
	}
	return nil
}

func isMissingRemote(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) &&
		commandErr.ExitCode >= 0 &&
		!commandErr.TimedOut &&
		(strings.Contains(commandErr.Stderr, "No such remote") ||
			strings.Contains(commandErr.Stderr, "no such remote"))
}

// Adopt makes the local branch track remote's tip, adopting fetched history as
// the branch's own. Used by a fresh store joining a remote that already has
// commits. It is only sound with no conflicting local work: the mirror file is
// regenerated on every export, so resetting to the remote tip is safe there.
func (c Client) Adopt(ctx context.Context, dir, remote, branch string) error {
	if remote == "" || branch == "" {
		return errors.New("adopt Git history: empty remote or branch")
	}
	if _, err := c.run(ctx, dir, c.networkTimeout(), false,
		"fetch", "--quiet", "--no-tags", "--end-of-options", remote, "refs/heads/"+branch); err != nil {
		return fmt.Errorf("fetch Git mirror: %w", err)
	}
	if _, err := c.run(ctx, dir, c.localTimeout(), false,
		"checkout", "-q", "-B", branch, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("adopt fetched Git head: %w", err)
	}
	return nil
}

// FastForward points the local branch at sha, adopting fetched history without a
// merge. It is for the common pull case where the remote is a strict descendant
// of the local head, so no merge commit is warranted.
func (c Client) FastForward(ctx context.Context, dir, branch, sha string) error {
	if branch == "" || sha == "" {
		return errors.New("fast-forward Git history: empty branch or sha")
	}
	if _, err := c.run(ctx, dir, c.localTimeout(), false,
		"checkout", "-q", "-B", branch, sha); err != nil {
		return fmt.Errorf("fast-forward to %s: %w", sha, err)
	}
	return nil
}
