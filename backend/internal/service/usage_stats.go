package service

import (
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

type providerUsageState struct {
	ProviderID   string
	ProviderType string
	Stats        session.UsageStats
}

type ProviderUsageInsight struct {
	ProviderKey  string
	ProviderID   string
	ProviderType string
	Usage        session.UsageStats
}

func normalizeUsageScope(scope string) string {
	scope = strings.TrimSpace(strings.ToLower(scope))
	if scope == "" {
		return "session"
	}
	switch scope {
	case "assistant_snapshot", "message", "message_usage":
		return "turn"
	case "rate_limit", "rate_limits":
		return "account"
	case "model", "model_list", "model_availability":
		return "models"
	case "capability":
		return "capabilities"
	}
	return scope
}

func isProviderScopedUsage(scope string) bool {
	scope = normalizeUsageScope(scope)
	switch scope {
	case "provider", "account", "global", "models", "capabilities":
		return true
	default:
		return false
	}
}

func providerUsageKey(providerID, providerType string) string {
	if id := strings.TrimSpace(providerID); id != "" {
		return "id:" + id
	}
	if kind := strings.TrimSpace(providerType); kind != "" {
		return "type:" + kind
	}
	return "provider:unknown"
}

func cloneUsageStats(in session.UsageStats) session.UsageStats {
	out := session.UsageStats{LastUpdatedAt: in.LastUpdatedAt}
	if len(in.ByScope) == 0 {
		return out
	}
	out.ByScope = make(map[string]session.UsageStat, len(in.ByScope))
	for k, v := range in.ByScope {
		entry := v
		if len(v.Metadata) > 0 {
			entry.Metadata = make(map[string]any, len(v.Metadata))
			for mk, mv := range v.Metadata {
				entry.Metadata[mk] = mv
			}
		}
		out.ByScope[k] = entry
	}
	return out
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out
}

func (e *AgentExecutor) applyResourceUsage(sc *sessionContext, scope string, data any, metadata map[string]any, at time.Time) {
	if e == nil || sc == nil || sc.session == nil {
		return
	}
	scope = normalizeUsageScope(scope)
	if at.IsZero() {
		at = time.Now()
	}

	usage := session.UsageStat{
		Scope:     scope,
		Data:      data,
		Metadata:  cloneMetadata(metadata),
		UpdatedAt: at,
	}

	e.usageMu.Lock()
	defer e.usageMu.Unlock()

	if e.sessionUsageStats == nil {
		e.sessionUsageStats = make(map[string]session.UsageStats)
	}
	sessionStats := e.sessionUsageStats[sc.session.ID]
	if sessionStats.ByScope == nil {
		sessionStats.ByScope = make(map[string]session.UsageStat)
	}
	sessionStats.ByScope[scope] = usage
	sessionStats.LastUpdatedAt = at
	e.sessionUsageStats[sc.session.ID] = sessionStats

	if !isProviderScopedUsage(scope) {
		return
	}

	if e.providerUsageStats == nil {
		e.providerUsageStats = make(map[string]providerUsageState)
	}
	providerID := strings.TrimSpace(sc.session.PreferredProviderID)
	providerType := strings.TrimSpace(sc.session.ProviderType)
	key := providerUsageKey(providerID, providerType)
	providerStats := e.providerUsageStats[key]
	providerStats.ProviderID = providerID
	providerStats.ProviderType = providerType
	if providerStats.Stats.ByScope == nil {
		providerStats.Stats.ByScope = make(map[string]session.UsageStat)
	}
	providerStats.Stats.ByScope[scope] = usage
	providerStats.Stats.LastUpdatedAt = at
	e.providerUsageStats[key] = providerStats
}

func (e *AgentExecutor) usageSnapshots(sessionID, providerID, providerType string) (session.UsageStats, session.UsageStats) {
	if e == nil {
		return session.UsageStats{}, session.UsageStats{}
	}
	key := providerUsageKey(providerID, providerType)

	e.usageMu.RLock()
	defer e.usageMu.RUnlock()

	sessionStats := cloneUsageStats(e.sessionUsageStats[sessionID])
	providerStats := session.UsageStats{}
	if state, ok := e.providerUsageStats[key]; ok {
		providerStats = cloneUsageStats(state.Stats)
	}
	return sessionStats, providerStats
}

func (e *AgentExecutor) ListProviderUsageInsights() []ProviderUsageInsight {
	if e == nil {
		return nil
	}
	e.usageMu.RLock()
	defer e.usageMu.RUnlock()
	insights := make([]ProviderUsageInsight, 0, len(e.providerUsageStats))
	for key, state := range e.providerUsageStats {
		insights = append(insights, ProviderUsageInsight{
			ProviderKey:  key,
			ProviderID:   state.ProviderID,
			ProviderType: state.ProviderType,
			Usage:        cloneUsageStats(state.Stats),
		})
	}
	return insights
}
