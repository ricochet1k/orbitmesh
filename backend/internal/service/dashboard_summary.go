package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	api "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	dashboardCommitScanLimit     = 50
	dashboardActivityLimit       = 12
	dashboardActionLimit         = 8
	dashboardHotspotLimit        = 8
	dashboardRecentFindingsLimit = 6
)

type dashboardSessionReader interface {
	ListSessions() []*domain.Session
	DeriveSessionState(sessionID string) (domain.SessionState, error)
}

type DashboardSummaryService struct {
	sessions dashboardSessionReader
	gitDir   string
	now      func() time.Time
}

func NewDashboardSummaryService(sessions dashboardSessionReader, gitDir string) *DashboardSummaryService {
	return &DashboardSummaryService{
		sessions: sessions,
		gitDir:   gitDir,
		now:      time.Now,
	}
}

func (s *DashboardSummaryService) Build(_ context.Context) api.DashboardSummaryResponse {
	summary := api.DashboardSummaryResponse{
		GeneratedAt: s.now().UTC(),
		Activity:    []api.DashboardActivityItem{},
		Actions:     []api.DashboardActionItem{},
		Hotspots:    []api.DashboardHotspotSummary{},
		Codeflow: api.DashboardCodeflow{
			OpenFindingsBySeverity: map[string]int{},
			RecentFindings:         []api.DashboardCodeflowFindingSummary{},
		},
	}

	s.populateSessionSignals(&summary)
	findingCountsByFile := s.populateCodeflowSignals(&summary)
	s.populateGitSignals(&summary, findingCountsByFile)

	sort.Slice(summary.Activity, func(i, j int) bool {
		return summary.Activity[i].Timestamp.After(summary.Activity[j].Timestamp)
	})
	if len(summary.Activity) > dashboardActivityLimit {
		summary.Activity = summary.Activity[:dashboardActivityLimit]
	}
	sort.Slice(summary.Actions, func(i, j int) bool {
		if summary.Actions[i].Score == summary.Actions[j].Score {
			return summary.Actions[i].Label < summary.Actions[j].Label
		}
		return summary.Actions[i].Score > summary.Actions[j].Score
	})
	if len(summary.Actions) > dashboardActionLimit {
		summary.Actions = summary.Actions[:dashboardActionLimit]
	}

	return summary
}

func (s *DashboardSummaryService) populateSessionSignals(summary *api.DashboardSummaryResponse) {
	if s.sessions == nil {
		action := api.DashboardActionItem{
			ID:     "sessions:overview",
			Kind:   "open_sessions",
			Label:  "Review live sessions",
			Target: "/sessions",
		}
		scoreAndExplainAction(&action, s.now().UTC(), time.Time{})
		summary.Actions = append(summary.Actions, action)
		return
	}

	sessions := s.sessions.ListSessions()
	summary.Pulse.SessionsTotal = len(sessions)
	for _, sess := range sessions {
		state := sess.GetState()
		if derived, err := s.sessions.DeriveSessionState(sess.ID); err == nil {
			state = derived
		}
		switch state {
		case domain.SessionStateRunning:
			summary.Pulse.SessionsRunning++
		case domain.SessionStateSuspended:
			summary.Pulse.SessionsSuspended++
		case domain.SessionStateIdle:
			summary.Pulse.SessionsIdle++
		default:
			summary.Pulse.SessionsOther++
		}

		snap := sess.Snapshot()
		summary.Activity = append(summary.Activity, api.DashboardActivityItem{
			ID:        "session:" + snap.ID,
			Kind:      "session",
			Title:     "Session " + snap.ID,
			Detail:    state.String(),
			Timestamp: snap.UpdatedAt,
		})

		if state == domain.SessionStateSuspended {
			action := api.DashboardActionItem{
				ID:      "resume:" + snap.ID,
				Kind:    "resume_session",
				Label:   "Resume suspended session",
				Session: snap.ID,
				Target:  "/sessions/" + snap.ID,
			}
			scoreAndExplainAction(&action, s.now().UTC(), snap.UpdatedAt)
			summary.Actions = append(summary.Actions, action)
		}
		if snap.CurrentTask != "" {
			action := api.DashboardActionItem{
				ID:      "task:" + snap.ID,
				Kind:    "inspect_task",
				Label:   snap.CurrentTask,
				Session: snap.ID,
				Target:  "/sessions/" + snap.ID,
			}
			scoreAndExplainAction(&action, s.now().UTC(), snap.UpdatedAt)
			summary.Actions = append(summary.Actions, action)
		}
	}

	if len(summary.Actions) == 0 {
		action := api.DashboardActionItem{
			ID:     "sessions:overview",
			Kind:   "open_sessions",
			Label:  "Review live sessions",
			Target: "/sessions",
		}
		scoreAndExplainAction(&action, s.now().UTC(), time.Time{})
		summary.Actions = append(summary.Actions, action)
	}
}

func (s *DashboardSummaryService) populateCodeflowSignals(summary *api.DashboardSummaryResponse) map[string]int {
	findingsByFile := map[string]int{}
	dbPath, ok := s.dashboardCodeflowDBPath()
	if !ok {
		return findingsByFile
	}
	report, err := codeflowmvp.ReadFindings(codeflowmvp.FindingsOptions{DBPath: dbPath})
	if err != nil {
		return findingsByFile
	}

	recent := make([]api.DashboardCodeflowFindingSummary, 0, minInt(dashboardRecentFindingsLimit, len(report.Findings)))
	openBySeverity := map[string]int{}
	now := s.now().UTC()

	findings := make([]codeflowmvp.StoredFinding, len(report.Findings))
	copy(findings, report.Findings)
	sort.Slice(findings, func(i, j int) bool {
		iEpoch := parseScanEpoch(findings[i].ScanEpoch)
		jEpoch := parseScanEpoch(findings[j].ScanEpoch)
		if iEpoch.Equal(jEpoch) {
			iRank := severityRank(findings[i].Severity)
			jRank := severityRank(findings[j].Severity)
			if iRank == jRank {
				return findings[i].ID < findings[j].ID
			}
			return iRank > jRank
		}
		if iEpoch.IsZero() {
			return false
		}
		if jEpoch.IsZero() {
			return true
		}
		return iEpoch.After(jEpoch)
	})

	for idx, finding := range findings {
		severity := normalizeFindingSeverity(finding.Severity)
		status := strings.ToLower(strings.TrimSpace(finding.Status))
		if status == "" {
			status = "open"
		}
		if status == "open" {
			summary.Codeflow.OpenFindings++
			openBySeverity[severity]++
			if finding.FileID != "" {
				findingsByFile[finding.FileID]++
			}
		}

		scanEpoch := parseScanEpoch(finding.ScanEpoch)
		if !scanEpoch.IsZero() && now.Sub(scanEpoch) <= 24*time.Hour {
			summary.Codeflow.RecentFindingActivity++
		}

		if idx < dashboardRecentFindingsLimit {
			recent = append(recent, api.DashboardCodeflowFindingSummary{
				ID:        finding.ID,
				Severity:  severity,
				Message:   finding.Message,
				FileID:    finding.FileID,
				Line:      finding.Line,
				Status:    status,
				ScanEpoch: scanEpoch,
			})
		}

		if !scanEpoch.IsZero() && idx < 3 {
			detail := severity
			if finding.FileID != "" {
				detail += " - " + finding.FileID
			}
			summary.Activity = append(summary.Activity, api.DashboardActivityItem{
				ID:        "finding:" + finding.ID,
				Kind:      "finding",
				Title:     finding.Message,
				Detail:    detail,
				Timestamp: scanEpoch,
			})
		}
	}

	summary.Codeflow.OpenFindingsBySeverity = openBySeverity
	summary.Codeflow.RecentFindings = recent
	return findingsByFile
}

func (s *DashboardSummaryService) dashboardCodeflowDBPath() (string, bool) {
	if s.gitDir == "" {
		return "", false
	}
	dbPath := filepath.Join(s.gitDir, ".codeflow-mvp.goraphdb")
	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}
	return dbPath, true
}

func (s *DashboardSummaryService) populateGitSignals(summary *api.DashboardSummaryResponse, findingsByFile map[string]int) {
	if s.gitDir == "" {
		s.addFindingOnlyHotspots(summary, findingsByFile)
		return
	}
	if _, err := os.Stat(filepath.Join(s.gitDir, ".git")); err != nil {
		s.addFindingOnlyHotspots(summary, findingsByFile)
		return
	}

	repo, err := git.PlainOpen(s.gitDir)
	if err != nil {
		s.addFindingOnlyHotspots(summary, findingsByFile)
		return
	}
	iter, err := repo.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		s.addFindingOnlyHotspots(summary, findingsByFile)
		return
	}

	authors := make(map[string]struct{})
	hotspots := make(map[string]api.DashboardHotspotSummary)
	now := s.now().UTC()
	seen := 0
	_ = iter.ForEach(func(commit *object.Commit) error {
		if seen >= dashboardCommitScanLimit {
			return nil
		}
		seen++
		summary.Codeflow.RecentCommits++
		if now.Sub(commit.Committer.When.UTC()) <= 24*time.Hour {
			summary.Codeflow.Commits24h++
		}
		authors[commit.Author.Email] = struct{}{}
		summary.Activity = append(summary.Activity, api.DashboardActivityItem{
			ID:        "commit:" + commit.Hash.String(),
			Kind:      "commit",
			Title:     shortCommitMessage(commit.Message),
			Detail:    shortHash(commit.Hash.String()),
			Timestamp: commit.Committer.When.UTC(),
		})

		stats, err := commit.Stats()
		if err != nil {
			return nil
		}
		for _, stat := range stats {
			entry := hotspots[stat.Name]
			entry.Path = stat.Name
			entry.Touches++
			entry.Churn += stat.Addition + stat.Deletion
			hotspots[stat.Name] = entry
		}
		return nil
	})

	summary.Codeflow.ActiveAuthors = len(authors)
	summary.Hotspots = make([]api.DashboardHotspotSummary, 0, len(hotspots))
	for _, hotspot := range hotspots {
		summary.Hotspots = append(summary.Hotspots, hotspot)
	}
	for path, count := range findingsByFile {
		entry := hotspots[path]
		entry.Path = path
		entry.Findings = count
		hotspots[path] = entry
	}
	summary.Hotspots = summary.Hotspots[:0]
	for _, hotspot := range hotspots {
		summary.Hotspots = append(summary.Hotspots, hotspot)
	}
	sort.Slice(summary.Hotspots, func(i, j int) bool {
		if summary.Hotspots[i].Churn == summary.Hotspots[j].Churn {
			if summary.Hotspots[i].Findings == summary.Hotspots[j].Findings {
				return summary.Hotspots[i].Path < summary.Hotspots[j].Path
			}
			return summary.Hotspots[i].Findings > summary.Hotspots[j].Findings
		}
		return summary.Hotspots[i].Churn > summary.Hotspots[j].Churn
	})
	if len(summary.Hotspots) > dashboardHotspotLimit {
		summary.Hotspots = summary.Hotspots[:dashboardHotspotLimit]
	}
}

func (s *DashboardSummaryService) addFindingOnlyHotspots(summary *api.DashboardSummaryResponse, findingsByFile map[string]int) {
	if len(findingsByFile) == 0 {
		return
	}
	summary.Hotspots = make([]api.DashboardHotspotSummary, 0, len(findingsByFile))
	for path, count := range findingsByFile {
		summary.Hotspots = append(summary.Hotspots, api.DashboardHotspotSummary{
			Path:     path,
			Findings: count,
		})
	}
	sort.Slice(summary.Hotspots, func(i, j int) bool {
		if summary.Hotspots[i].Findings == summary.Hotspots[j].Findings {
			return summary.Hotspots[i].Path < summary.Hotspots[j].Path
		}
		return summary.Hotspots[i].Findings > summary.Hotspots[j].Findings
	})
	if len(summary.Hotspots) > dashboardHotspotLimit {
		summary.Hotspots = summary.Hotspots[:dashboardHotspotLimit]
	}
}

func scoreAndExplainAction(action *api.DashboardActionItem, now, updatedAt time.Time) {
	score := 20
	reasons := []string{}
	switch action.Kind {
	case "resume_session":
		score += 50
		reasons = append(reasons, "Session is suspended and needs manual resume")
	case "inspect_task":
		score += 20
		reasons = append(reasons, "Session has an active task worth checking")
	default:
		reasons = append(reasons, "Useful baseline workflow check")
	}
	if !updatedAt.IsZero() {
		age := now.Sub(updatedAt.UTC())
		switch {
		case age <= 30*time.Minute:
			score += 20
			reasons = append(reasons, "Updated in the last 30 minutes")
		case age <= 2*time.Hour:
			score += 10
			reasons = append(reasons, "Updated in the last 2 hours")
		}
	}
	if score > 100 {
		score = 100
	}
	action.Score = score
	action.Rationale = strings.Join(reasons, "; ")
}

func normalizeFindingSeverity(input string) string {
	severity := strings.ToLower(strings.TrimSpace(input))
	if severity == "" {
		return "unknown"
	}
	return severity
}

func severityRank(input string) int {
	switch normalizeFindingSeverity(input) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

func parseScanEpoch(scanEpoch string) time.Time {
	value := strings.TrimSpace(scanEpoch)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func shortCommitMessage(message string) string {
	line := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	if line == "" {
		return "Commit"
	}
	return line
}

func shortHash(sha string) string {
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}
