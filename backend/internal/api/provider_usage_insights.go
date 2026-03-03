package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/provider/common/codex"
	"github.com/ricochet1k/orbitmesh/internal/session"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	usageScopeModels       = "models"
	usageScopeCapabilities = "capabilities"
)

type modelOption struct {
	ID       string
	Label    string
	Metadata map[string]any
}

func (h *Handler) providerUsageInsights(ctx context.Context) []apiTypes.ProviderUsageInsight {
	insights := h.executor.ListProviderUsageInsights()
	byKey := make(map[string]*apiTypes.ProviderUsageInsight, len(insights))
	providers := make([]apiTypes.ProviderUsageInsight, 0, len(insights))

	for _, insight := range insights {
		providers = append(providers, apiTypes.ProviderUsageInsight{
			ProviderKey:  insight.ProviderKey,
			ProviderID:   insight.ProviderID,
			ProviderType: insight.ProviderType,
			Usage:        usageStatsToAPI(insight.Usage),
		})
		byKey[insight.ProviderKey] = &providers[len(providers)-1]
	}

	if h.providerStorage == nil {
		return providers
	}

	configs, err := h.providerStorage.List()
	if err != nil {
		return providers
	}

	for _, cfg := range configs {
		key := providerUsageInsightKey(cfg)
		entry, ok := byKey[key]
		if !ok {
			providers = append(providers, apiTypes.ProviderUsageInsight{
				ProviderKey:  key,
				ProviderID:   strings.TrimSpace(cfg.ID),
				ProviderType: strings.TrimSpace(cfg.Type),
			})
			entry = &providers[len(providers)-1]
			byKey[key] = entry
		}
		mergeUsageMetadata(&entry.Usage, providerDiscoveryUsage(ctx, cfg))
	}

	return providers
}

func providerUsageInsightKey(cfg storage.ProviderConfig) string {
	id := strings.TrimSpace(cfg.ID)
	if id != "" {
		return "id:" + id
	}
	typ := strings.TrimSpace(cfg.Type)
	if typ != "" {
		return "type:" + typ
	}
	return "provider:unknown"
}

func mergeUsageMetadata(dst *apiTypes.UsageStats, src session.UsageStats) {
	if dst == nil || len(src.ByScope) == 0 {
		return
	}
	if dst.ByScope == nil {
		dst.ByScope = map[string]apiTypes.UsageStat{}
	}
	for scope, stat := range src.ByScope {
		dst.ByScope[scope] = apiTypes.UsageStat{
			Scope:     stat.Scope,
			Data:      stat.Data,
			Metadata:  stat.Metadata,
			UpdatedAt: stat.UpdatedAt,
		}
		if stat.UpdatedAt.After(dst.LastUpdatedAt) {
			dst.LastUpdatedAt = stat.UpdatedAt
		}
	}
}

func providerDiscoveryUsage(ctx context.Context, cfg storage.ProviderConfig) session.UsageStats {
	now := time.Now().UTC()
	stats := session.UsageStats{ByScope: map[string]session.UsageStat{}, LastUpdatedAt: now}

	var models []modelOption
	var currentModel string
	status := "unsupported"
	reason := "model discovery is not available"
	source := "none"

	capabilities := map[string]any{}
	providerType := strings.TrimSpace(cfg.Type)

	switch providerType {
	case "openai":
		models, currentModel, status, reason, source = discoverOpenAIModels(ctx, cfg)
		capabilities["account_quota_progress"] = map[string]any{
			"supported": false,
			"status":    "unknown",
			"reason":    "OpenAI account quota progress is not exposed via a stable API",
		}
	case "codex":
		models, currentModel, status, reason, source = discoverCodexModels(ctx, cfg)
	case "claude", "claude-ws":
		currentModel = trimString(customValue(cfg.Custom, "model"))
		if currentModel != "" {
			models = []modelOption{{ID: currentModel, Label: currentModel}}
			status = "partial"
			reason = "using configured model; runtime may expose additional models"
			source = "provider_config"
		}
	case "acp":
		models, currentModel = extractACPModelOptions(cfg.Custom)
		if len(models) > 0 {
			status = "partial"
			reason = "using ACP config options from provider state"
			source = "acp_config_options"
		} else {
			status = "unsupported"
			reason = "ACP model options were not provided by the agent"
			source = "none"
		}
		capabilities["provider_usage_limits"] = map[string]any{
			"supported": false,
			"status":    "unsupported",
			"reason":    "ACP usage limits are only available when the agent emits explicit limit updates",
		}
	}

	if len(models) == 0 {
		if configured := trimString(customValue(cfg.Custom, "model")); configured != "" {
			models = append(models, modelOption{ID: configured, Label: configured, Metadata: map[string]any{"source": "provider_config"}})
			if currentModel == "" {
				currentModel = configured
			}
			if status == "unsupported" {
				status = "partial"
				reason = "model/list is unavailable; showing configured model"
				source = "provider_config"
			}
		}
	}

	if len(models) > 0 || status != "unsupported" || providerType == "openai" || providerType == "acp" {
		stats.ByScope[usageScopeModels] = session.UsageStat{
			Scope: usageScopeModels,
			Data: map[string]any{
				"current_model":    currentModel,
				"available_models": modelOptionsToPayload(models),
				"discovery": map[string]any{
					"supported": status == "ok" || status == "partial",
					"status":    status,
					"reason":    reason,
					"source":    source,
				},
			},
			UpdatedAt: now,
		}
	}

	if len(capabilities) > 0 {
		stats.ByScope[usageScopeCapabilities] = session.UsageStat{
			Scope:     usageScopeCapabilities,
			Data:      capabilities,
			UpdatedAt: now,
		}
	}

	return stats
}

func discoverOpenAIModels(ctx context.Context, cfg storage.ProviderConfig) ([]modelOption, string, string, string, string) {
	apiKey := strings.TrimSpace(firstNonEmpty(cfg.Env["OPENAI_API_KEY"], os.Getenv("OPENAI_API_KEY")))
	baseURL := strings.TrimSpace(firstNonEmpty(asString(customValue(cfg.Custom, "base_url"), ""), cfg.Env["OPENAI_BASE_URL"]))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(baseURL, "/") {
		baseURL = strings.TrimSuffix(baseURL, "/")
	}
	endpoint := baseURL
	if !strings.HasSuffix(strings.ToLower(endpoint), "/v1") {
		endpoint += "/v1"
	}
	endpoint += "/models"

	if apiKey == "" && strings.Contains(baseURL, "api.openai.com") {
		return nil, "", "unsupported", "OPENAI_API_KEY is not configured", "config"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", "unsupported", err.Error(), "http_request"
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "unsupported", fmt.Sprintf("model discovery request failed: %v", err), "http_request"
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "unsupported", fmt.Sprintf("model discovery returned HTTP %d", resp.StatusCode), "http_request"
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, "", "unsupported", fmt.Sprintf("failed to decode model list: %v", err), "http_decode"
	}

	models := make([]modelOption, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		models = append(models, modelOption{ID: id, Label: id})
	}
	slices.SortFunc(models, func(a, b modelOption) int { return strings.Compare(a.ID, b.ID) })

	current := trimString(customValue(cfg.Custom, "model"))
	if current == "" && len(models) > 0 {
		current = models[0].ID
	}
	if len(models) == 0 {
		return nil, current, "unsupported", "provider returned an empty model list", "openai_models_api"
	}
	return models, current, "ok", "", "openai_models_api"
}

func discoverCodexModels(ctx context.Context, cfg storage.ProviderConfig) ([]modelOption, string, string, string, string) {
	staticCfg := codex.Config{Environment: cfg.Env}
	if cfg.Custom != nil {
		if v := trimString(customValue(cfg.Custom, "codex_command")); v != "" {
			staticCfg.Command = v
		}
		if args := splitArgs(customValue(cfg.Custom, "codex_args")); len(args) > 0 {
			staticCfg.Args = args
		}
	}
	sessionCfg := session.Config{Custom: cfg.Custom, Environment: cfg.Env}
	models, source, err := codex.DiscoverModels(ctx, staticCfg, sessionCfg)
	if err == nil && len(models) > 0 {
		out := make([]modelOption, 0, len(models))
		for _, id := range models {
			out = append(out, modelOption{ID: id, Label: id})
		}
		current := trimString(customValue(cfg.Custom, "model"))
		if current == "" {
			current = out[0].ID
		}
		return out, current, "ok", "", source
	}

	current := trimString(customValue(cfg.Custom, "model"))
	if current == "" {
		return nil, "", "unsupported", "codex model/list is unavailable and no fallback model is configured", source
	}
	reason := "using configured Codex model fallback"
	if err != nil {
		reason = fmt.Sprintf("model/list unavailable (%v); using configured model fallback", err)
	}
	return []modelOption{{ID: current, Label: current}}, current, "partial", reason, "provider_config"
}

func extractACPModelOptions(custom map[string]any) ([]modelOption, string) {
	var out []modelOption
	var current string
	for _, key := range []string{"configOptions", "config_options", "models", "model_options"} {
		value, ok := custom[key]
		if !ok {
			continue
		}
		extracted, curr := scanModelOptions(value)
		out = append(out, extracted...)
		if current == "" {
			current = curr
		}
	}
	if current == "" {
		current = trimString(customValue(custom, "model"))
	}
	return dedupeModelOptions(out), current
}

func scanModelOptions(value any) ([]modelOption, string) {
	var out []modelOption
	current := ""
	walkAny(value, func(node map[string]any) {
		category := strings.ToLower(trimString(node["category"]))
		id := trimString(firstNonEmpty(trimString(node["id"]), trimString(node["modelId"]), trimString(node["model_id"])))
		label := trimString(firstNonEmpty(trimString(node["name"]), trimString(node["title"]), id))
		if category == "model" && id != "" {
			out = append(out, modelOption{ID: id, Label: label})
		}
		if current == "" {
			if v := trimString(firstNonEmpty(trimString(node["currentModelId"]), trimString(node["current_model_id"]))); v != "" {
				current = v
			}
		}
	})
	return out, current
}

func modelOptionsToPayload(models []modelOption) []map[string]any {
	if len(models) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(models))
	for _, m := range dedupeModelOptions(models) {
		entry := map[string]any{"id": m.ID, "label": m.Label}
		if len(m.Metadata) > 0 {
			entry["metadata"] = m.Metadata
		}
		out = append(out, entry)
	}
	return out
}

func dedupeModelOptions(models []modelOption) []modelOption {
	if len(models) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]modelOption, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(model.Label) == "" {
			model.Label = id
		}
		model.ID = id
		out = append(out, model)
	}
	return out
}

func customValue(custom map[string]any, key string) any {
	if custom == nil {
		return nil
	}
	return custom[key]
}

func asString(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

func trimString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func walkAny(value any, visit func(map[string]any)) {
	switch node := value.(type) {
	case map[string]any:
		visit(node)
		for _, child := range node {
			walkAny(child, visit)
		}
	case []any:
		for _, child := range node {
			walkAny(child, visit)
		}
	}
}

func splitArgs(value any) []string {
	switch v := value.(type) {
	case string:
		parts := strings.Fields(v)
		if len(parts) == 0 {
			return nil
		}
		return parts
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}
