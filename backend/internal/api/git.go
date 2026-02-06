package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	defaultCommitLimit = 25
	maxCommitLimit     = 200
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{6,40}$`)

func (h *Handler) listCommits(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"))
	commits, err := gitLog(h.gitDir, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load commits", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.CommitListResponse{Commits: commits})
}

func (h *Handler) getCommit(w http.ResponseWriter, r *http.Request) {
	sha := chi.URLParam(r, "sha")
	if !shaPattern.MatchString(sha) {
		writeError(w, http.StatusBadRequest, "invalid commit sha", "")
		return
	}

	commit, err := gitShow(h.gitDir, sha)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load commit", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.CommitDetailResponse{Commit: commit})
}

func parseLimit(raw string) int {
	if raw == "" {
		return defaultCommitLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultCommitLimit
	}
	if limit > maxCommitLimit {
		return maxCommitLimit
	}
	return limit
}

func gitLog(dir string, limit int) ([]apiTypes.CommitSummary, error) {
	cmd := exec.Command(
		"git",
		"log",
		"--no-color",
		"--date=iso-strict",
		"--pretty=format:%H%x1f%an%x1f%ae%x1f%ad%x1f%s",
		"-n",
		strconv.Itoa(limit),
	)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []apiTypes.CommitSummary{}, nil
	}

	commits := make([]apiTypes.CommitSummary, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		timestamp := parseCommitTime(parts[3])
		commits = append(commits, apiTypes.CommitSummary{
			Sha:       parts[0],
			Author:    parts[1],
			Email:     parts[2],
			Timestamp: timestamp,
			Message:   parts[4],
		})
	}

	return commits, nil
}

func gitShow(dir, sha string) (apiTypes.CommitDetail, error) {
	cmd := exec.Command(
		"git",
		"show",
		"--no-color",
		"--date=iso-strict",
		"--pretty=format:%H%x1f%an%x1f%ae%x1f%ad%x1f%s%x1e",
		"-U3",
		sha,
	)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return apiTypes.CommitDetail{}, err
	}

	chunks := strings.SplitN(out.String(), "\x1e", 2)
	if len(chunks) < 2 {
		return apiTypes.CommitDetail{}, fmt.Errorf("unexpected git output")
	}
	meta := strings.TrimSpace(chunks[0])
	diff := strings.TrimSpace(chunks[1])
	fields := strings.SplitN(meta, "\x1f", 5)
	if len(fields) < 5 {
		return apiTypes.CommitDetail{}, fmt.Errorf("invalid commit metadata")
	}

	return apiTypes.CommitDetail{
		Sha:       fields[0],
		Author:    fields[1],
		Email:     fields[2],
		Timestamp: parseCommitTime(fields[3]),
		Message:   fields[4],
		Diff:      diff,
		Files:     extractDiffFiles(diff),
	}, nil
}

func parseCommitTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	timestamp, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return timestamp
}

func extractDiffFiles(diff string) []string {
	lines := strings.Split(diff, "\n")
	files := make([]string, 0, 6)
	seen := map[string]struct{}{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		path := strings.TrimPrefix(fields[2], "a/")
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	return files
}
