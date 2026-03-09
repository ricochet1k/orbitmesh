package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp/rules"
)

func TestDashboardSummaryService_BuildIncludesGitSignals(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	first := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Hour)
	_ = commitFile(t, repo, dir, "hot/file.txt", "hello\n", "first change", first)
	_ = commitFile(t, repo, dir, "hot/file.txt", "hello\nworld\n", "second change", second)

	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return second.Add(time.Hour) }

	summary := svc.Build(context.Background(), DashboardTimeWindow7d)

	if summary.Window != "7d" {
		t.Fatalf("window = %q, want 7d", summary.Window)
	}

	if summary.Codeflow.RecentCommits != 2 {
		t.Fatalf("recent commits = %d, want 2", summary.Codeflow.RecentCommits)
	}
	if summary.Codeflow.CommitsInWindow != 2 {
		t.Fatalf("commitsInWindow = %d, want 2", summary.Codeflow.CommitsInWindow)
	}
	if summary.Codeflow.Commits24h != 2 {
		t.Fatalf("commits24h = %d, want 2", summary.Codeflow.Commits24h)
	}
	if summary.Codeflow.ActiveAuthors != 1 {
		t.Fatalf("active authors = %d, want 1", summary.Codeflow.ActiveAuthors)
	}

	foundCommit := false
	for _, item := range summary.Activity {
		if item.Kind == "commit" && item.Title == "second change" {
			foundCommit = true
			break
		}
	}
	if !foundCommit {
		t.Fatalf("expected commit activity entry, got %+v", summary.Activity)
	}

	if len(summary.Hotspots) == 0 {
		t.Fatalf("expected hotspots, got none")
	}
	if summary.Hotspots[0].Path != "hot/file.txt" {
		t.Fatalf("hotspot path = %q, want hot/file.txt", summary.Hotspots[0].Path)
	}
	if summary.Hotspots[0].Touches != 2 {
		t.Fatalf("touches = %d, want 2", summary.Hotspots[0].Touches)
	}
	if len(summary.Actions) == 0 {
		t.Fatalf("expected at least one action")
	}
	if summary.Actions[0].Score == 0 {
		t.Fatalf("expected action score to be populated")
	}
	if summary.Actions[0].Rationale == "" {
		t.Fatalf("expected action rationale to be populated")
	}
	if summary.Actions[0].Category == "" {
		t.Fatalf("expected action category to be populated")
	}
	if summary.Actions[0].Summary == "" {
		t.Fatalf("expected action summary to be populated")
	}
	if summary.Actions[0].SuggestedNextStep == "" {
		t.Fatalf("expected action suggested next step to be populated")
	}
}

func TestDashboardSummaryService_BuildCodeflowFallbackWhenDBMissing(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	_ = commitFile(t, repo, dir, "hot/file.txt", "hello\n", "change", now.Add(-time.Hour))

	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return now }

	summary := svc.Build(context.Background(), DashboardTimeWindow7d)

	if summary.Codeflow.OpenFindings != 0 {
		t.Fatalf("open findings = %d, want 0", summary.Codeflow.OpenFindings)
	}
	if summary.Codeflow.RecentFindingActivity != 0 {
		t.Fatalf("recent finding activity = %d, want 0", summary.Codeflow.RecentFindingActivity)
	}
	if len(summary.Codeflow.FindingsBySeverity) != 0 {
		t.Fatalf("expected empty findingsBySeverity map when DB absent")
	}
	if len(summary.Codeflow.OpenFindingsBySeverity) != 0 {
		t.Fatalf("expected empty severity map when DB absent")
	}
	if len(summary.Codeflow.RecentFindings) != 0 {
		t.Fatalf("expected no recent findings when DB absent")
	}
	if len(summary.Codeflow.TopRiskPaths) != 0 {
		t.Fatalf("expected no top risk paths when DB absent")
	}
}

func TestDashboardSummaryService_BuildIntegratesCodeflowFindings(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	_ = commitFile(t, repo, dir, "hot/file.txt", "hello\n", "change", now.Add(-2*time.Hour))

	if _, err := codeflowmvp.PersistExtraction(codeflowmvp.ExtractionSummary{
		Files: []codeflowmvp.FileFact{{ID: "hot/file.txt", Path: "hot/file.txt", PackageID: "pkg"}},
		Findings: []rules.Finding{
			{
				RuleID:      "dangerous_pattern",
				Severity:    "high",
				Message:     "dangerous call detected",
				Fingerprint: "abc123",
				Confidence:  0.92,
				Location: rules.Location{
					FileID: "hot/file.txt",
					Start:  rules.Position{Line: 8, Column: 2},
					End:    rules.Position{Line: 8, Column: 12},
				},
			},
		},
	}, codeflowmvp.PersistOptions{
		DBPath:    filepath.Join(dir, ".codeflow-mvp.goraphdb"),
		ScanEpoch: now.Add(-time.Hour).Format(time.RFC3339Nano),
		Producer:  "test-producer",
	}); err != nil {
		t.Fatalf("persist extraction: %v", err)
	}

	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return now }

	summary := svc.Build(context.Background(), DashboardTimeWindow7d)

	if summary.Codeflow.OpenFindings != 1 {
		t.Fatalf("open findings = %d, want 1", summary.Codeflow.OpenFindings)
	}
	if summary.Codeflow.OpenFindingsBySeverity["high"] != 1 {
		t.Fatalf("high severity findings = %d, want 1", summary.Codeflow.OpenFindingsBySeverity["high"])
	}
	if summary.Codeflow.FindingsBySeverity["high"] != 1 {
		t.Fatalf("high severity findingsBySeverity = %d, want 1", summary.Codeflow.FindingsBySeverity["high"])
	}
	if summary.Codeflow.RecentFindingActivity != 1 {
		t.Fatalf("recent finding activity = %d, want 1", summary.Codeflow.RecentFindingActivity)
	}
	if summary.Codeflow.NewFindings != 1 {
		t.Fatalf("new findings = %d, want 1", summary.Codeflow.NewFindings)
	}
	if len(summary.Codeflow.RecentFindings) != 1 {
		t.Fatalf("recent findings len = %d, want 1", len(summary.Codeflow.RecentFindings))
	}
	if len(summary.Codeflow.TopRiskPaths) != 1 {
		t.Fatalf("topRiskPaths len = %d, want 1", len(summary.Codeflow.TopRiskPaths))
	}
	if len(summary.Hotspots) == 0 || summary.Hotspots[0].Findings == 0 {
		t.Fatalf("expected hotspot findings to be populated, got %+v", summary.Hotspots)
	}
}

func TestDashboardSummaryService_BuildWindowingAffectsGitMetrics(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	_ = commitFile(t, repo, dir, "old/file.txt", "v1\n", "old change", now.Add(-48*time.Hour))
	_ = commitFile(t, repo, dir, "recent/file.txt", "v2\n", "recent change", now.Add(-2*time.Hour))

	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return now }

	daySummary := svc.Build(context.Background(), DashboardTimeWindow24h)
	if daySummary.Codeflow.CommitsInWindow != 1 {
		t.Fatalf("24h commitsInWindow = %d, want 1", daySummary.Codeflow.CommitsInWindow)
	}
	if len(daySummary.Hotspots) == 0 || daySummary.Hotspots[0].Path != "recent/file.txt" {
		t.Fatalf("24h hotspot should only include recent file, got %+v", daySummary.Hotspots)
	}

	weekSummary := svc.Build(context.Background(), DashboardTimeWindow7d)
	if weekSummary.Codeflow.CommitsInWindow != 2 {
		t.Fatalf("7d commitsInWindow = %d, want 2", weekSummary.Codeflow.CommitsInWindow)
	}
}

func TestDashboardSummaryService_BuildCachesByWindowWithTTL(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	baseNow := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	_ = commitFile(t, repo, dir, "hot/file.txt", "v1\n", "first", baseNow.Add(-time.Hour))

	currentNow := baseNow
	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return currentNow }

	first := svc.Build(context.Background(), DashboardTimeWindow7d)
	if first.Codeflow.CommitsInWindow != 1 {
		t.Fatalf("first commitsInWindow = %d, want 1", first.Codeflow.CommitsInWindow)
	}

	_ = commitFile(t, repo, dir, "hot/file.txt", "v2\n", "second", baseNow)

	second := svc.Build(context.Background(), DashboardTimeWindow7d)
	if second.Codeflow.CommitsInWindow != 1 {
		t.Fatalf("cached commitsInWindow = %d, want 1", second.Codeflow.CommitsInWindow)
	}

	currentNow = currentNow.Add(25 * time.Second)
	third := svc.Build(context.Background(), DashboardTimeWindow7d)
	if third.Codeflow.CommitsInWindow != 2 {
		t.Fatalf("post-ttl commitsInWindow = %d, want 2", third.Codeflow.CommitsInWindow)
	}

	daySummary := svc.Build(context.Background(), DashboardTimeWindow24h)
	if daySummary.Window != "24h" {
		t.Fatalf("window = %q, want 24h", daySummary.Window)
	}
}

func TestDashboardSummaryService_BuildComputesComplexityRankings(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	content := "package sample\n\ntype Worker struct{ Name string }\n\nfunc (w *Worker) Run(v int) int {\n\tif v > 1 && v < 10 {\n\t\tfor i := 0; i < v; i++ {\n\t\t\tv += i\n\t\t}\n\t}\n\treturn v\n}\n\nfunc helper(x int) int {\n\tif x%2 == 0 {\n\t\treturn x + 1\n\t}\n\treturn x\n}\n"
	_ = commitFile(t, repo, dir, "pkg/sample.go", content, "add sample go file", now.Add(-time.Hour))

	extraction, err := codeflowmvp.ScanPath(dir, filepath.Join(dir, "pkg"))
	if err != nil {
		t.Fatalf("scan path: %v", err)
	}
	if _, err := codeflowmvp.PersistExtraction(extraction, codeflowmvp.PersistOptions{DBPath: filepath.Join(dir, ".codeflow-mvp.goraphdb")}); err != nil {
		t.Fatalf("persist extraction: %v", err)
	}

	svc := NewDashboardSummaryService(nil, dir)
	svc.now = func() time.Time { return now }

	summary := svc.Build(context.Background(), DashboardTimeWindow7d)
	if len(summary.Codeflow.MostComplexFunctions) == 0 {
		t.Fatalf("expected most complex functions to be populated")
	}
	if len(summary.Codeflow.MostComplexModules) == 0 {
		t.Fatalf("expected most complex modules to be populated")
	}
	if len(summary.Codeflow.MostComplexTypes) == 0 {
		t.Fatalf("expected most complex types to be populated")
	}
}

func commitFile(t *testing.T, repo *git.Repository, dir, path, content, message string, when time.Time) string {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(path); err != nil {
		t.Fatalf("add: %v", err)
	}

	sig := &object.Signature{Name: "Test User", Email: "test@example.com", When: when}
	hash, err := wt.Commit(message, &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return hash.String()
}
