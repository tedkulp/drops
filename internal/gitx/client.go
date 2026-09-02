package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultLocalTimeout   = 10 * time.Second
	defaultNetworkTimeout = 30 * time.Second
	defaultWaitDelay      = time.Second
)

// Client runs Git with the non-interactive process policy required by drops.
// Its zero value is ready to use.
type Client struct {
	LocalTimeout   time.Duration
	NetworkTimeout time.Duration
	WaitDelay      time.Duration
}

// Facts describes the Git repository containing a working directory.
type Facts struct {
	InRepository bool
	Root         string
	RemoteURL    string
}

// CommandError is a deterministic account of a failed Git invocation.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	TimedOut bool
	cause    error
}

func (e *CommandError) Error() string {
	command := "git " + strings.Join(e.Args, " ")
	if e.TimedOut {
		return command + ": timed out"
	}
	if errors.Is(e.cause, context.Canceled) {
		return command + ": canceled"
	}
	if e.Stderr != "" {
		return fmt.Sprintf("%s: exit %d: %s", command, e.ExitCode, e.Stderr)
	}
	return fmt.Sprintf("%s: exit %d", command, e.ExitCode)
}

func (e *CommandError) Unwrap() error { return e.cause }

// Inspect returns the main repository root and origin URL for dir. A directory
// outside a Git repository is a normal zero-Facts result.
func (c Client) Inspect(ctx context.Context, dir string) (Facts, error) {
	commonDir, err := c.run(ctx, dir, c.localTimeout(), true,
		"rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		if isNotRepository(err) {
			return Facts{}, nil
		}
		return Facts{}, err
	}

	root := filepath.Clean(filepath.Dir(commonDir))
	facts := Facts{InRepository: true, Root: root}
	remote, err := c.run(ctx, dir, c.localTimeout(), true, "remote", "get-url", "origin")
	if err == nil {
		facts.RemoteURL = remote
		return facts, nil
	}
	if isMissingOrigin(err) {
		return facts, nil
	}
	return Facts{}, err
}

func isNotRepository(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) &&
		commandErr.ExitCode >= 0 &&
		!commandErr.TimedOut &&
		strings.Contains(strings.ToLower(commandErr.Stderr), "not a git repository")
}

func isMissingOrigin(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) &&
		commandErr.ExitCode >= 0 &&
		!commandErr.TimedOut &&
		strings.Contains(strings.ToLower(commandErr.Stderr), "no such remote 'origin'")
}

func (c Client) run(
	ctx context.Context,
	dir string,
	timeout time.Duration,
	readOnly bool,
	args ...string,
) (string, error) {
	out, err := c.runBytes(ctx, dir, timeout, readOnly, args...)
	return strings.TrimSpace(string(out)), err
}

func (c Client) runBytes(
	ctx context.Context,
	dir string,
	timeout time.Duration,
	readOnly bool,
	args ...string,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fullArgs := make([]string, 0, len(args)+6)
	fullArgs = append(fullArgs,
		"-c", "commit.gpgsign=false",
		"-c", "gc.autoDetach=true",
		"-c", "core.hooksPath=/dev/null",
	)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.WaitDelay = c.waitDelay()
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if readOnly {
		cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	}

	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}

	commandErr := &CommandError{
		Args:     append([]string(nil), args...),
		ExitCode: -1,
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, exec.ErrWaitDelay),
		cause:    err,
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		commandErr.cause = ctxErr
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		commandErr.ExitCode = exitErr.ExitCode()
		commandErr.Stderr = strings.TrimSpace(string(exitErr.Stderr))
	}
	return nil, commandErr
}

func (c Client) localTimeout() time.Duration {
	if c.LocalTimeout > 0 {
		return c.LocalTimeout
	}
	return defaultLocalTimeout
}

func (c Client) networkTimeout() time.Duration {
	if c.NetworkTimeout > 0 {
		return c.NetworkTimeout
	}
	return defaultNetworkTimeout
}

func (c Client) waitDelay() time.Duration {
	if c.WaitDelay > 0 {
		return c.WaitDelay
	}
	return defaultWaitDelay
}
