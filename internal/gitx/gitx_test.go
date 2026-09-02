package gitx_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/gitx"
)

func TestInspectFindsMainRepositoryFromLinkedWorktree(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, "", "init", "-q", "-b", "main", main)
	runGit(t, main, "config", "user.name", "test")
	runGit(t, main, "config", "user.email", "test@example.com")
	runGit(t, main, "remote", "add", "origin", "ssh://git@example.test/team/repo.git")
	runGit(t, main, "commit", "--allow-empty", "-q", "-m", "base")
	runGit(t, main, "worktree", "add", "-q", "-b", "linked", linked)

	facts, err := (gitx.Client{}).Inspect(context.Background(), linked)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !facts.InRepository {
		t.Fatal("Inspect reported a linked worktree as outside Git")
	}
	if facts.Root != main {
		t.Fatalf("Root = %q, want main repository %q", facts.Root, main)
	}
	if facts.RemoteURL != "ssh://git@example.test/team/repo.git" {
		t.Fatalf("RemoteURL = %q", facts.RemoteURL)
	}
}

func TestInspectDistinguishesNoRepositoryFromBrokenRepository(t *testing.T) {
	client := gitx.Client{}
	plain := t.TempDir()
	facts, err := client.Inspect(context.Background(), plain)
	if err != nil {
		t.Fatalf("Inspect outside Git: %v", err)
	}
	if facts.InRepository {
		t.Fatalf("Inspect outside Git = %#v", facts)
	}

	broken := t.TempDir()
	runGit(t, broken, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(broken, ".git", "config"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = client.Inspect(context.Background(), broken)
	if err == nil {
		t.Fatal("Inspect returned nil error for a corrupt repository")
	}
	var commandErr *gitx.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("Inspect error %T is not a CommandError: %v", err, err)
	}
	if commandErr.ExitCode <= 0 || commandErr.Stderr == "" {
		t.Fatalf("CommandError lacks stable exit facts: %#v", commandErr)
	}
	if got := strings.Join(commandErr.Args, " "); got != "rev-parse --path-format=absolute --git-common-dir" {
		t.Fatalf("CommandError args = %q", got)
	}
}

func TestCommitPushFetchAndReadFile(t *testing.T) {
	ctx := context.Background()
	client := gitx.Client{}
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	runGit(t, "", "init", "--bare", "-q", "-b", "main", remote)

	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := client.Ensure(ctx, gitx.RepositoryConfig{Dir: dir}); err != nil {
			t.Fatalf("Ensure(%s): %v", filepath.Base(dir), err)
		}
		runGit(t, dir, "remote", "add", "origin", remote)
	}

	const contents = "{\"id\":\"abc\"}\n"
	if err := os.WriteFile(filepath.Join(first, "issues.jsonl"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "drops.db"), []byte("not a mirror"), 0o600); err != nil {
		t.Fatal(err)
	}

	commit, err := client.Commit(ctx, first, "export mirror", []string{"issues.jsonl"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !commit.Created || len(commit.SHA) != 40 {
		t.Fatalf("Commit = %#v, want a new full-SHA commit", commit)
	}
	if got := runGit(t, first, "ls-files"); got != "issues.jsonl" {
		t.Fatalf("tracked files = %q, want only the explicit mirror path", got)
	}
	unchanged, err := client.Commit(ctx, first, "must not be empty", []string{"issues.jsonl"})
	if err != nil {
		t.Fatalf("unchanged Commit: %v", err)
	}
	if unchanged.Created {
		t.Fatalf("unchanged Commit created %#v", unchanged)
	}

	if err := client.Push(ctx, first, "origin", "main"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	fetched, err := client.Fetch(ctx, second, "origin", "main")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if fetched.SHA != commit.SHA {
		t.Fatalf("fetched SHA = %q, want %q", fetched.SHA, commit.SHA)
	}
	got, err := client.ReadFile(ctx, second, fetched.SHA, "issues.jsonl")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != contents {
		t.Fatalf("ReadFile = %q, want %q", got, contents)
	}
}

func TestCommitSurvivesHostileGlobalConfigAndIgnore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	hooks := filepath.Join(home, "hooks")
	if err := os.Mkdir(hooks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	excludes := filepath.Join(home, "global-ignore")
	if err := os.WriteFile(excludes, []byte("*.jsonl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, setting := range [][2]string{
		{"commit.gpgsign", "true"},
		{"gpg.format", "ssh"},
		{"gpg.ssh.program", filepath.Join(home, "missing-signer")},
		{"core.hooksPath", hooks},
		{"core.excludesFile", excludes},
	} {
		runGit(t, "", "config", "--global", setting[0], setting[1])
	}

	repo := filepath.Join(home, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	client := gitx.Client{}
	if err := client.Ensure(context.Background(), gitx.RepositoryConfig{Dir: repo}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "issues.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit, err := client.Commit(context.Background(), repo, "mirror", []string{"issues.jsonl"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !commit.Created || !commit.Forced {
		t.Fatalf("Commit = %#v, want created and forced past the global ignore", commit)
	}
}

func TestMergeCommitJoinsDivergedHistories(t *testing.T) {
	ctx := context.Background()
	client := gitx.Client{}
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	runGit(t, "", "init", "--bare", "-q", "-b", "main", remote)
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := client.Ensure(ctx, gitx.RepositoryConfig{Dir: dir}); err != nil {
			t.Fatalf("Ensure(%s): %v", filepath.Base(dir), err)
		}
		runGit(t, dir, "remote", "add", "origin", remote)
	}

	path := "issues.jsonl"
	if err := os.WriteFile(filepath.Join(first, path), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseCommit, err := client.Commit(ctx, first, "base", []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, first, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(ctx, second, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	runGit(t, second, "checkout", "-q", "-B", "main", "FETCH_HEAD")

	if err := os.WriteFile(filepath.Join(first, path), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	local, err := client.Commit(ctx, first, "local", []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, path), []byte("remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteCommit, err := client.Commit(ctx, second, "remote", []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, second, "origin", "main"); err != nil {
		t.Fatal(err)
	}

	fetched, err := client.Fetch(ctx, first, "origin", "main")
	if err != nil {
		t.Fatal(err)
	}
	if fetched.SHA != remoteCommit.SHA {
		t.Fatalf("fetched SHA = %q, want %q", fetched.SHA, remoteCommit.SHA)
	}
	if err := os.WriteFile(filepath.Join(first, path), []byte("resolved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	merged, err := client.MergeCommit(ctx, first, "resolve replicas", []string{path}, fetched.SHA)
	if err != nil {
		t.Fatalf("MergeCommit: %v", err)
	}
	parents := strings.Fields(runGit(t, first, "rev-list", "--parents", "-n", "1", merged.SHA))
	want := []string{merged.SHA, local.SHA, remoteCommit.SHA}
	if strings.Join(parents, " ") != strings.Join(want, " ") {
		t.Fatalf("merge ancestry = %q, want %q (base %s)", parents, want, baseCommit.SHA)
	}
	if err := client.Push(ctx, first, "origin", "main"); err != nil {
		t.Fatalf("push resolved merge: %v", err)
	}
}

func TestWaitDelayBoundsGrandchildHoldingOutput(t *testing.T) {
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	script := "#!/bin/sh\n/usr/bin/sleep 30 &\nexit 0\n"
	if err := os.WriteFile(gitPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	client := gitx.Client{NetworkTimeout: 2 * time.Second, WaitDelay: 20 * time.Millisecond}
	_, err := client.Fetch(context.Background(), t.TempDir(), "origin", "main")
	if err == nil {
		t.Fatal("Fetch returned nil while a grandchild held its output pipe")
	}
	var commandErr *gitx.CommandError
	if !errors.As(err, &commandErr) || !commandErr.TimedOut {
		t.Fatalf("Fetch error = %#v, want a timed-out CommandError", err)
	}
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Fetch error does not retain exec.ErrWaitDelay: %v", err)
	}
}

func TestNetworkTimeoutIsReportedAsContextDeadline(t *testing.T) {
	binDir := t.TempDir()
	gitPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	client := gitx.Client{NetworkTimeout: 20 * time.Millisecond, WaitDelay: 20 * time.Millisecond}
	_, err := client.Fetch(context.Background(), t.TempDir(), "origin", "main")
	if err == nil {
		t.Fatal("Fetch returned nil error after its deadline")
	}
	var commandErr *gitx.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("Fetch error %T is not a CommandError: %v", err, err)
	}
	if !commandErr.TimedOut {
		t.Fatalf("CommandError = %#v, want TimedOut", commandErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch error does not unwrap to context deadline: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmdArgs := append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
