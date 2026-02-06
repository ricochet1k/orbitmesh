package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func TestCommitEndpoints(t *testing.T) {
	repoDir, sha := setupGitRepo(t)

	env := newTestEnv(t)
	env.handler.gitDir = repoDir
	r := env.router()

	t.Run("list commits", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/commits?limit=1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp apiTypes.CommitListResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Commits) != 1 {
			t.Fatalf("expected 1 commit, got %d", len(resp.Commits))
		}
	})

	t.Run("get commit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/commits/"+sha, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp apiTypes.CommitDetailResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Commit.Sha != sha {
			t.Fatalf("commit sha = %q, want %q", resp.Commit.Sha, sha)
		}
		if !strings.Contains(resp.Commit.Diff, "diff --git") {
			t.Fatalf("expected diff content, got %q", resp.Commit.Diff)
		}
		if len(resp.Commit.Files) == 0 || resp.Commit.Files[0] != "demo.txt" {
			t.Fatalf("expected files to include demo.txt, got %v", resp.Commit.Files)
		}
	})

	t.Run("reject invalid sha", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/commits/not-a-sha", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestParseLimit(t *testing.T) {
	if got := parseLimit(""); got != defaultCommitLimit {
		t.Fatalf("parseLimit(\"\") = %d, want %d", got, defaultCommitLimit)
	}
	if got := parseLimit("-5"); got != defaultCommitLimit {
		t.Fatalf("parseLimit(-5) = %d, want %d", got, defaultCommitLimit)
	}
	if got := parseLimit("999"); got != maxCommitLimit {
		t.Fatalf("parseLimit(999) = %d, want %d", got, maxCommitLimit)
	}
}

func TestExtractDiffFiles(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/demo.txt b/demo.txt",
		"index 111..222 100644",
		"--- a/demo.txt",
		"+++ b/demo.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"diff --git a/second.txt b/second.txt",
	}, "\n")
	files := extractDiffFiles(diff)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0] != "demo.txt" || files[1] != "second.txt" {
		t.Fatalf("unexpected files: %v", files)
	}
}

func setupGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	demoPath := filepath.Join(dir, "demo.txt")
	if err := os.WriteFile(demoPath, []byte("one\n"), 0644); err != nil {
		t.Fatalf("write demo file: %v", err)
	}
	runGit(t, dir, "add", "demo.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	if err := os.WriteFile(demoPath, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatalf("write demo file: %v", err)
	}
	runGit(t, dir, "add", "demo.txt")
	runGit(t, dir, "commit", "-m", "add line")

	sha := runGit(t, dir, "rev-parse", "HEAD")
	return dir, sha
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out.String())
	}
	return strings.TrimSpace(out.String())
}
