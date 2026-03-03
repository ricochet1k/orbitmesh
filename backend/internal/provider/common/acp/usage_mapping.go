package acp

import (
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/ricochet1k/orbitmesh/internal/domain"
)

func modelUsageFromACPState(state *acpsdk.SessionModelState, source string) (domain.ResourceUsageData, bool) {
	if state == nil {
		return domain.ResourceUsageData{}, false
	}
	models := make([]map[string]any, 0, len(state.AvailableModels))
	for _, model := range state.AvailableModels {
		id := strings.TrimSpace(string(model.ModelId))
		if id == "" {
			continue
		}
		label := strings.TrimSpace(model.Name)
		if label == "" {
			label = id
		}
		models = append(models, map[string]any{"id": id, "label": label})
	}
	current := strings.TrimSpace(string(state.CurrentModelId))
	if len(models) == 0 && current == "" {
		return domain.ResourceUsageData{}, false
	}
	return domain.ResourceUsageData{
		Scope: "models",
		Data: map[string]any{
			"source":           source,
			"current_model":    current,
			"available_models": models,
		},
	}, true
}

func parseACPUpdateMeta(meta any) (domain.ResourceUsageData, bool) {
	metaMap, ok := meta.(map[string]any)
	if !ok || len(metaMap) == 0 {
		return domain.ResourceUsageData{}, false
	}

	for _, key := range []string{"configOptions", "config_options", "modelOptions", "model_options"} {
		if value, ok := metaMap[key]; ok {
			if usage, ok := parseACPModelOptionsUsage(metaMap, "config_options_update"); ok {
				return usage, true
			}
			if usage, ok := parseACPModelOptionsUsage(value, "config_options_update"); ok {
				return usage, true
			}
		}
	}

	for _, key := range []string{"usageLimits", "usage_limits", "rateLimits", "rate_limits"} {
		if value, ok := metaMap[key]; ok {
			return domain.ResourceUsageData{
				Scope: "account",
				Data:  value,
				Metadata: map[string]any{
					"source": "config_options_update",
				},
			}, true
		}
	}

	return domain.ResourceUsageData{}, false
}

func parseACPModelOptionsUsage(value any, source string) (domain.ResourceUsageData, bool) {
	options, current := extractModelOptions(value)
	if len(options) == 0 && current == "" {
		return domain.ResourceUsageData{}, false
	}
	return domain.ResourceUsageData{
		Scope: "models",
		Data: map[string]any{
			"source":           source,
			"current_model":    current,
			"available_models": options,
		},
	}, true
}

func extractModelOptions(value any) ([]map[string]any, string) {
	seen := map[string]struct{}{}
	options := []map[string]any{}
	current := ""
	walkACPValue(value, func(node map[string]any) {
		if current == "" {
			for _, key := range []string{"currentModelId", "current_model_id", "current_model"} {
				if v, ok := node[key].(string); ok && strings.TrimSpace(v) != "" {
					current = strings.TrimSpace(v)
					break
				}
			}
		}

		category := ""
		if c, ok := node["category"].(string); ok {
			category = strings.ToLower(strings.TrimSpace(c))
		}
		id := ""
		for _, key := range []string{"id", "modelId", "model_id", "value"} {
			if v, ok := node[key].(string); ok && strings.TrimSpace(v) != "" {
				id = strings.TrimSpace(v)
				break
			}
		}
		if category != "model" || id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		label := id
		for _, key := range []string{"name", "label", "title"} {
			if v, ok := node[key].(string); ok && strings.TrimSpace(v) != "" {
				label = strings.TrimSpace(v)
				break
			}
		}
		options = append(options, map[string]any{"id": id, "label": label})
	})
	return options, current
}

func walkACPValue(value any, visit func(map[string]any)) {
	switch node := value.(type) {
	case map[string]any:
		visit(node)
		for _, child := range node {
			walkACPValue(child, visit)
		}
	case []any:
		for _, child := range node {
			walkACPValue(child, visit)
		}
	}
}
