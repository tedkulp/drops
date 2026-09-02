package gitx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tedkulp/drops/internal/gitx"
)

func TestHeadEmptyUntilFirstCommit(t *testing.T) {
	client := gitx.Client{}
	dir := t.TempDir()
	if err := client.Ensure(context.Background(), gitx.RepositoryConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	head, err := client.Head(context.Background(), dir)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "" {
		t.Fatalf("Head = %q, want empty on an unborn branch", head)
	}

	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit, err := client.Commit(context.Background(), dir, "first", []string{"f"})
	if err != nil {
		t.Fatal(err)
	}
	head, err = client.Head(context.Background(), dir)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != commit.SHA {
		t.Fatalf("Head = %q, want %q", head, commit.SHA)
	}
}

func TestSetRemoteURLAddThenReplace(t *testing.T) {
	client := gitx.Client{}
	dir := t.TempDir()
	if err := client.Ensure(context.Background(), gitx.RepositoryConfig{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.SetRemoteURL(ctx, dir, "origin", "/tmp/a.git"); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := client.SetRemoteURL(ctx, dir, "origin", "/tmp/a.git"); err != nil {
		t.Fatalf("idempotent set remote: %v", err)
	}
	if err := client.SetRemoteURL(ctx, dir, "origin", "/tmp/b.git"); err != nil {
		t.Fatalf("replace remote: %v", err)
	}
	if got := runGit(t, dir, "remote", "get-url", "origin"); got != "/tmp/b.git" {
		t.Fatalf("remote url = %q, want /tmp/b.git", got)
	}
}

func TestAdoptAdoptsRemoteTip(t *testing.T) {
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
			t.Fatal(err)
		}
		if err := client.SetRemoteURL(ctx, dir, "origin", remote); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(first, "f"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit, err := client.Commit(ctx, first, "one", []string{"f"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, first, "origin", "main"); err != nil {
		t.Fatal(err)
	}

	if err := client.Adopt(ctx, second, "origin", "main"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	head, err := client.Head(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if head != commit.SHA {
		t.Fatalf("adopted head = %q, want %q", head, commit.SHA)
	}
	body, err := client.ReadFile(ctx, second, head, "f")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "one\n" {
		t.Fatalf("adopted file = %q, want %q", body, "one\n")
	}
}
