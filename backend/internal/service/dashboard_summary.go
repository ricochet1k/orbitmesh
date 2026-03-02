package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
	api "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	dashboardCommitScanLimit     = 50
	dashboardActivityLimit       = 12
	dashboardActionLimit         = 8
	dashboardHotspotLimit        = 8
	dashboardRecentFindingsLimit = 6
	dashboardComplexityLimit     = 6
	dashboardSummaryCacheTTL     = 20 * time.Second
)

type DashboardTimeWindow string

const (
	DashboardTimeWindow24h DashboardTimeWindow = "24h"
	DashboardTimeWindow7d  DashboardTimeWindow = "7d"
)

func ParseDashboardTimeWindow(raw string) DashboardTimeWindow {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(DashboardTimeWindow24h):
		return DashboardTimeWindow24h
	case string(DashboardTimeWindow7d):
		return DashboardTimeWindow7d
	default:
		return DashboardTimeWindow7d
	}
}

func (w DashboardTimeWindow) Duration() time.Duration {
	if w == DashboardTimeWindow24h {
		return 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

func (w DashboardTimeWindow) String() string {
	return string(ParseDashboardTimeWindow(string(w)))
}

type dashboardSessionReader interface {
	ListSessions() []*domain.Session
	DeriveSessionState(sessionID string) (domain.SessionState, error)
}

type DashboardSummaryService struct {
	sessions dashboardSessionReader
	gitDir   string
	now      func() time.Time
	cacheTTL time.Duration

	cacheMu         sync.RWMutex
	cache           map[DashboardTimeWindow]dashboardSummaryCacheEntry
	scanMu          sync.Mutex
	scanning        bool
	autoScanEnabled bool
}

type dashboardSummaryCacheEntry struct {
	expiresAt time.Time
	summary   api.DashboardSummaryResponse
}

func NewDashboardSummaryService(sessions dashboardSessionReader, gitDir string) *DashboardSummaryService {
	return &DashboardSummaryService{
		sessions:        sessions,
		gitDir:          gitDir,
		now:             time.Now,
		cacheTTL:        dashboardSummaryCacheTTL,
		cache:           map[DashboardTimeWindow]dashboardSummaryCacheEntry{},
		autoScanEnabled: false,
	}
}

func (s *DashboardSummaryService) SetAutoScanEnabled(enabled bool) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	s.autoScanEnabled = enabled
}

func (s *DashboardSummaryService) Build(_ context.Context, window DashboardTimeWindow) api.DashboardSummaryResponse {
	window = ParseDashboardTimeWindow(window.String())
	now := s.now().UTC()

	s.cacheMu.RLock()
	cacheEntry, ok := s.cache[window]
	s.cacheMu.RUnlock()
	if ok && now.Before(cacheEntry.expiresAt) {
		return cloneDashboardSummary(cacheEntry.summary)
	}

	windowStart := now.Add(-window.Duration())
	summary := api.DashboardSummaryResponse{
		GeneratedAt: now,
		Window:      window.String(),
		Activity:    []api.DashboardActivityItem{},
		Actions:     []api.DashboardActionItem{},
		Hotspots:    []api.DashboardHotspotSummary{},
		Codeflow: api.DashboardCodeflow{
			FindingsBySeverity:     map[string]int{},
			OpenFindingsBySeverity: map[string]int{},
			RecentFindings:         []api.DashboardCodeflowFindingSummary{},
			TopRiskPaths:           []api.DashboardCodeflowRiskPath{},
			MostComplexFunctions:   []api.DashboardCodeflowComplexItem{},
			MostComplexModules:     []api.DashboardCodeflowComplexItem{},
			MostComplexTypes:       []api.DashboardCodeflowComplexItem{},
		},
	}

	s.populateSessionSignals(&summary, windowStart)
	findingCountsByFile := s.populateCodeflowSignals(&summary, windowStart)
	s.populateGitSignals(&summary, findingCountsByFile, windowStart)
	s.populateCodeComplexitySignals(&summary)
	s.maybeTriggerCodeflowScan(&summary, now)
	s.addCodeflowRunActionIfMissing(&summary)

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

	s.cacheMu.Lock()
	s.cache[window] = dashboardSummaryCacheEntry{
		expiresAt: now.Add(s.cacheTTL),
		summary:   cloneDashboardSummary(summary),
	}
	s.cacheMu.Unlock()

	return summary
}

func (s *DashboardSummaryService) populateSessionSignals(summary *api.DashboardSummaryResponse, windowStart time.Time) {
	if s.sessions == nil {
		action := api.DashboardActionItem{
			ID:                "sessions:overview",
			Kind:              "open_sessions",
			Category:          "sessions",
			Severity:          "low",
			Label:             "Review recent sessions",
			Summary:           "No targeted follow-ups were found.",
			Details:           "Open the sessions list to pick a run that needs attention.",
			Evidence:          []string{"No suspended sessions", "No active task hints"},
			SuggestedNextStep: "Open sessions and inspect latest run status.",
			Href:              "/sessions",
			Target:            "/sessions",
			Timestamp:         s.now().UTC(),
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
		updatedAt := snap.UpdatedAt.UTC()
		if !updatedAt.Before(windowStart) {
			summary.Activity = append(summary.Activity, api.DashboardActivityItem{
				ID:        "session:" + snap.ID,
				Kind:      "session",
				Title:     "Session " + snap.ID,
				Detail:    state.String(),
				Timestamp: updatedAt,
			})
		}

		if state == domain.SessionStateSuspended {
			suspensionReason := deriveSuspensionReason(snap)
			targetTitle := snap.Title
			if strings.TrimSpace(targetTitle) == "" {
				targetTitle = snap.ID
			}
			severity := "medium"
			if isDependencyWait(suspensionReason) {
				severity = "low"
			}
			action := api.DashboardActionItem{
				ID:                "resume:" + snap.ID,
				Kind:              "resume_session",
				Category:          "session_recovery",
				Severity:          severity,
				Label:             "Suspended session needs review",
				Summary:           targetTitle,
				Details:           "Suspension reason: " + suspensionReason,
				Evidence:          []string{"Session: " + snap.ID, "State: suspended", "Reason: " + suspensionReason},
				SuggestedNextStep: "Open the session and decide whether to resume or resolve the blocker.",
				TargetTitle:       targetTitle,
				SuspensionReason:  suspensionReason,
				Timestamp:         snap.UpdatedAt,
				Href:              "/sessions/" + snap.ID,
				Session:           snap.ID,
				Target:            "/sessions/" + snap.ID,
			}
			scoreAndExplainAction(&action, s.now().UTC(), snap.UpdatedAt)
			summary.Actions = append(summary.Actions, action)
		}
		if snap.CurrentTask != "" {
			targetTitle := snap.Title
			if strings.TrimSpace(targetTitle) == "" {
				targetTitle = snap.ID
			}
			action := api.DashboardActionItem{
				ID:                "task:" + snap.ID,
				Kind:              "inspect_task",
				Category:          "task_monitoring",
				Severity:          "low",
				Label:             "Task progress needs verification",
				Summary:           targetTitle + " - " + snap.CurrentTask,
				Details:           "Active task may need follow-up to avoid silent stalls.",
				Evidence:          []string{"Current task: " + snap.CurrentTask, "Session: " + snap.ID},
				SuggestedNextStep: "Open session details and verify task progress.",
				TargetTitle:       targetTitle,
				Timestamp:         snap.UpdatedAt,
				Href:              "/sessions/" + snap.ID,
				Session:           snap.ID,
				Target:            "/sessions/" + snap.ID,
			}
			scoreAndExplainAction(&action, s.now().UTC(), snap.UpdatedAt)
			summary.Actions = append(summary.Actions, action)
		}
	}

	if len(summary.Actions) == 0 {
		action := api.DashboardActionItem{
			ID:                "sessions:overview",
			Kind:              "open_sessions",
			Category:          "sessions",
			Severity:          "low",
			Label:             "Review recent sessions",
			Summary:           "No urgent session actions were detected.",
			Details:           "A quick review helps keep throughput and reliability healthy.",
			Evidence:          []string{"No suspended sessions", "No active task hints"},
			SuggestedNextStep: "Open sessions and inspect latest run status.",
			Href:              "/sessions",
			Target:            "/sessions",
			Timestamp:         s.now().UTC(),
		}
		scoreAndExplainAction(&action, s.now().UTC(), time.Time{})
		summary.Actions = append(summary.Actions, action)
	}
}

func (s *DashboardSummaryService) populateCodeflowSignals(summary *api.DashboardSummaryResponse, windowStart time.Time) map[string]int {
	findingsByFile := map[string]int{}
	dbPath, ok := s.dashboardCodeflowDBPath()
	if !ok {
		return findingsByFile
	}
	report, err := codeflowmvp.ReadFindings(codeflowmvp.FindingsOptions{DBPath: dbPath})
	if err != nil {
		return findingsByFile
	}
	summary.Codeflow.HasData = true

	recent := make([]api.DashboardCodeflowFindingSummary, 0, minInt(dashboardRecentFindingsLimit, len(report.Findings)))
	findingsBySeverity := map[string]int{}
	topRiskPathIndex := map[string]api.DashboardCodeflowRiskPath{}
	recentActivityCount := 0
	latestScan := time.Time{}

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

	for _, finding := range findings {
		severity := normalizeFindingSeverity(finding.Severity)
		status := strings.ToLower(strings.TrimSpace(finding.Status))
		if status == "" {
			status = "open"
		}
		scanEpoch := parseScanEpoch(finding.ScanEpoch)
		if scanEpoch.After(latestScan) {
			latestScan = scanEpoch
		}
		withinWindow := !scanEpoch.IsZero() && !scanEpoch.Before(windowStart)

		if status == "open" {
			summary.Codeflow.OpenFindings++
			findingsBySeverity[severity]++
			if finding.FileID != "" {
				findingsByFile[finding.FileID]++
				risk := topRiskPathIndex[finding.FileID]
				risk.Path = finding.FileID
				risk.OpenFindings++
				risk.RiskScore += severityRiskWeight(severity)
				topRiskPathIndex[finding.FileID] = risk
			}
			if withinWindow {
				summary.Codeflow.NewFindings++
			}
		} else if withinWindow {
			summary.Codeflow.ResolvedFindings++
		}

		if withinWindow {
			summary.Codeflow.RecentFindingActivity++
			if len(recent) < dashboardRecentFindingsLimit {
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
		}

		if withinWindow && recentActivityCount < 3 {
			recentActivityCount++
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

	summary.Codeflow.FindingsBySeverity = findingsBySeverity
	summary.Codeflow.OpenFindingsBySeverity = findingsBySeverity
	summary.Codeflow.RecentFindings = recent
	summary.Codeflow.LastScanAt = latestScan

	summary.Codeflow.TopRiskPaths = make([]api.DashboardCodeflowRiskPath, 0, len(topRiskPathIndex))
	for _, risk := range topRiskPathIndex {
		summary.Codeflow.TopRiskPaths = append(summary.Codeflow.TopRiskPaths, risk)
	}
	sort.Slice(summary.Codeflow.TopRiskPaths, func(i, j int) bool {
		if summary.Codeflow.TopRiskPaths[i].RiskScore == summary.Codeflow.TopRiskPaths[j].RiskScore {
			if summary.Codeflow.TopRiskPaths[i].OpenFindings == summary.Codeflow.TopRiskPaths[j].OpenFindings {
				return summary.Codeflow.TopRiskPaths[i].Path < summary.Codeflow.TopRiskPaths[j].Path
			}
			return summary.Codeflow.TopRiskPaths[i].OpenFindings > summary.Codeflow.TopRiskPaths[j].OpenFindings
		}
		return summary.Codeflow.TopRiskPaths[i].RiskScore > summary.Codeflow.TopRiskPaths[j].RiskScore
	})
	if len(summary.Codeflow.TopRiskPaths) > dashboardHotspotLimit {
		summary.Codeflow.TopRiskPaths = summary.Codeflow.TopRiskPaths[:dashboardHotspotLimit]
	}
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

func (s *DashboardSummaryService) populateGitSignals(summary *api.DashboardSummaryResponse, findingsByFile map[string]int, windowStart time.Time) {
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
			return storer.ErrStop
		}
		seen++
		commitTime := commit.Committer.When.UTC()
		if commitTime.Before(windowStart) {
			return storer.ErrStop
		}
		summary.Codeflow.RecentCommits++
		summary.Codeflow.CommitsInWindow++
		if now.Sub(commit.Committer.When.UTC()) <= 24*time.Hour {
			summary.Codeflow.Commits24h++
		}
		authors[commit.Author.Email] = struct{}{}
		summary.Activity = append(summary.Activity, api.DashboardActivityItem{
			ID:        "commit:" + commit.Hash.String(),
			Kind:      "commit",
			Title:     shortCommitMessage(commit.Message),
			Detail:    shortHash(commit.Hash.String()),
			Timestamp: commitTime,
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

func (s *DashboardSummaryService) addCodeflowRunActionIfMissing(summary *api.DashboardSummaryResponse) {
	if summary.Codeflow.HasData {
		return
	}
	action := api.DashboardActionItem{
		ID:                "codeflow:scan",
		Kind:              "run_codeflow_scan",
		Category:          "analysis",
		Severity:          "medium",
		Label:             "Run CodeFlow scan",
		Summary:           "No CodeFlow findings database detected.",
		Details:           "Run a quick scan to populate findings, risk paths, and code intelligence widgets.",
		Evidence:          []string{"Expected DB: .codeflow-mvp.goraphdb", "Current dashboard is using structural complexity only"},
		SuggestedNextStep: "Run: go run ./backend/cmd/codeflow-mvp scan --db ./.codeflow-mvp.goraphdb ./backend",
		Target:            "/",
		Timestamp:         s.now().UTC(),
	}
	scoreAndExplainAction(&action, s.now().UTC(), action.Timestamp)
	summary.Actions = append(summary.Actions, action)
}

func scoreAndExplainAction(action *api.DashboardActionItem, now, updatedAt time.Time) {
	score := 20
	reasons := []string{}
	switch strings.ToLower(strings.TrimSpace(action.Severity)) {
	case "critical":
		score += 60
		reasons = append(reasons, "Critical severity")
	case "high":
		score += 45
		reasons = append(reasons, "High severity")
	case "medium":
		score += 30
		reasons = append(reasons, "Medium severity")
	default:
		score += 15
		reasons = append(reasons, "Low severity")
	}
	if action.Kind == "resume_session" {
		reasons = append(reasons, "Suspended session awaiting decision")
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
	action.Timestamp = updatedAt.UTC()
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

func severityRiskWeight(input string) int {
	return severityRank(input) * 10
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

func deriveSuspensionReason(snap domain.SessionSnapshot) string {
	if ctx, ok := snap.SuspensionContext.(*session.SuspensionContext); ok && ctx != nil {
		if len(ctx.Dependencies) > 0 {
			return "waiting on dependency"
		}
		if strings.TrimSpace(ctx.Reason) != "" {
			return strings.TrimSpace(ctx.Reason)
		}
	}
	if ctx, ok := snap.SuspensionContext.(session.SuspensionContext); ok {
		if len(ctx.Dependencies) > 0 {
			return "waiting on dependency"
		}
		if strings.TrimSpace(ctx.Reason) != "" {
			return strings.TrimSpace(ctx.Reason)
		}
	}
	v := reflect.ValueOf(snap.SuspensionContext)
	if v.IsValid() {
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() == reflect.Struct {
			reason := v.FieldByName("Reason")
			if reason.IsValid() && reason.Kind() == reflect.String {
				text := strings.TrimSpace(reason.String())
				if text != "" {
					return text
				}
			}
			deps := v.FieldByName("Dependencies")
			if deps.IsValid() && deps.Kind() == reflect.Slice && deps.Len() > 0 {
				return "waiting on dependency"
			}
		}
	}
	for i := len(snap.Transitions) - 1; i >= 0; i-- {
		transition := snap.Transitions[i]
		if transition.To == domain.SessionStateSuspended {
			if strings.TrimSpace(transition.Reason) != "" {
				return strings.TrimSpace(transition.Reason)
			}
			break
		}
	}
	return "interrupted"
}

func isDependencyWait(reason string) bool {
	value := strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(value, "depend") || strings.Contains(value, "wait") || strings.Contains(value, "tool result")
}

func (s *DashboardSummaryService) populateCodeComplexitySignals(summary *api.DashboardSummaryResponse) {
	dbPath, ok := s.dashboardCodeflowDBPath()
	if !ok {
		return
	}
	report, err := codeflowmvp.ReadComplexity(codeflowmvp.ComplexityOptions{DBPath: dbPath, Limit: dashboardComplexityLimit})
	if err != nil {
		return
	}
	functions := make([]api.DashboardCodeflowComplexItem, 0, len(report.Functions))
	for _, item := range report.Functions {
		functions = append(functions, api.DashboardCodeflowComplexItem{Name: item.Name, Path: item.Path, Score: item.Score})
	}
	modules := make([]api.DashboardCodeflowComplexItem, 0, len(report.Modules))
	for _, item := range report.Modules {
		modules = append(modules, api.DashboardCodeflowComplexItem{Name: item.Name, Path: item.Path, Score: item.Score})
	}
	types := make([]api.DashboardCodeflowComplexItem, 0, len(report.Types))
	for _, item := range report.Types {
		types = append(types, api.DashboardCodeflowComplexItem{Name: item.Name, Path: item.Path, Score: item.Score})
	}
	summary.Codeflow.MostComplexFunctions = functions
	summary.Codeflow.MostComplexModules = modules
	summary.Codeflow.MostComplexTypes = types
}

func (s *DashboardSummaryService) maybeTriggerCodeflowScan(summary *api.DashboardSummaryResponse, now time.Time) {
	last := summary.Codeflow.LastScanAt
	stale := !summary.Codeflow.HasData || last.IsZero() || now.Sub(last) > time.Hour
	summary.Codeflow.ScanStale = stale
	if !stale || !s.autoScanEnabled {
		return
	}
	summary.Codeflow.AutoScanTriggered = s.TriggerCodeflowScanAsync()
}

func (s *DashboardSummaryService) TriggerCodeflowScanAsync() bool {
	if s.gitDir == "" {
		return false
	}
	s.scanMu.Lock()
	if s.scanning {
		s.scanMu.Unlock()
		return false
	}
	s.scanning = true
	s.scanMu.Unlock()

	go func() {
		defer func() {
			s.scanMu.Lock()
			s.scanning = false
			s.scanMu.Unlock()
		}()
		scanPath := filepath.Join(s.gitDir, "backend")
		if stat, err := os.Stat(scanPath); err != nil || !stat.IsDir() {
			scanPath = s.gitDir
		}
		extraction, err := codeflowmvp.ScanPath(scanPath)
		if err != nil {
			return
		}
		_, _ = codeflowmvp.PersistExtraction(extraction, codeflowmvp.PersistOptions{
			DBPath:   filepath.Join(s.gitDir, ".codeflow-mvp.goraphdb"),
			Producer: "dashboard-auto-scan",
		})
		s.cacheMu.Lock()
		s.cache = map[DashboardTimeWindow]dashboardSummaryCacheEntry{}
		s.cacheMu.Unlock()
	}()
	return true
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

func cloneDashboardSummary(in api.DashboardSummaryResponse) api.DashboardSummaryResponse {
	out := in
	out.Activity = append([]api.DashboardActivityItem(nil), in.Activity...)
	out.Actions = append([]api.DashboardActionItem(nil), in.Actions...)
	out.Hotspots = append([]api.DashboardHotspotSummary(nil), in.Hotspots...)
	out.Codeflow.RecentFindings = append([]api.DashboardCodeflowFindingSummary(nil), in.Codeflow.RecentFindings...)
	out.Codeflow.TopRiskPaths = append([]api.DashboardCodeflowRiskPath(nil), in.Codeflow.TopRiskPaths...)
	out.Codeflow.MostComplexFunctions = append([]api.DashboardCodeflowComplexItem(nil), in.Codeflow.MostComplexFunctions...)
	out.Codeflow.MostComplexModules = append([]api.DashboardCodeflowComplexItem(nil), in.Codeflow.MostComplexModules...)
	out.Codeflow.MostComplexTypes = append([]api.DashboardCodeflowComplexItem(nil), in.Codeflow.MostComplexTypes...)
	out.Codeflow.FindingsBySeverity = copyIntMap(in.Codeflow.FindingsBySeverity)
	out.Codeflow.OpenFindingsBySeverity = copyIntMap(in.Codeflow.OpenFindingsBySeverity)
	for idx := range out.Actions {
		out.Actions[idx].Evidence = append([]string(nil), in.Actions[idx].Evidence...)
	}
	return out
}

func copyIntMap(in map[string]int) map[string]int {
	if in == nil {
		return map[string]int{}
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
