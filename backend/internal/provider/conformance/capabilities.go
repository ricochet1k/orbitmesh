package conformance

import "strings"

const (
	ScenarioStatusPass            = "pass"
	ScenarioStatusPartial         = "partial"
	ScenarioStatusBlockedByConfig = "blocked_by_config"
	ScenarioStatusFail            = "fail"
	ScenarioStatusSkipped         = "skipped"
)

type ScenarioRequirement string

const (
	RequirementRequired    ScenarioRequirement = "required"
	RequirementOptional    ScenarioRequirement = "optional"
	RequirementUnsupported ScenarioRequirement = "unsupported"
)

type ProviderCapabilities struct {
	ProviderType         string
	Ignored              bool
	IgnoredReason        string
	ScenarioRequirements map[string]ScenarioRequirement
}

func capabilitiesForProvider(providerType string) ProviderCapabilities {
	providerType = strings.ToLower(strings.TrimSpace(providerType))
	capabilities := ProviderCapabilities{
		ProviderType: providerType,
		ScenarioRequirements: map[string]ScenarioRequirement{
			"startup_probe":      RequirementRequired,
			"message_roundtrip":  RequirementRequired,
			"reasoning_progress": RequirementOptional,
			"tool_call_flow":     RequirementOptional,
			"permission_flow":    RequirementOptional,
			"mcp_integration":    RequirementOptional,
			"raw_fidelity":       RequirementRequired,
		},
	}

	switch providerType {
	case "adk", "pty":
		capabilities.Ignored = true
		capabilities.IgnoredReason = "provider excluded from required conformance matrix"
		for scenarioID := range capabilities.ScenarioRequirements {
			capabilities.ScenarioRequirements[scenarioID] = RequirementUnsupported
		}
	case "openai":
		capabilities.ScenarioRequirements["permission_flow"] = RequirementUnsupported
	case "claude-ws", "codex", "acp":
		capabilities.ScenarioRequirements["reasoning_progress"] = RequirementRequired
		capabilities.ScenarioRequirements["tool_call_flow"] = RequirementRequired
	case "claude":
		capabilities.ScenarioRequirements["tool_call_flow"] = RequirementRequired
	}

	if providerType == "claude" || providerType == "claude-ws" || providerType == "codex" {
		capabilities.ScenarioRequirements["mcp_integration"] = RequirementUnsupported
	}

	if providerType == "codex" || providerType == "claude-ws" {
		capabilities.ScenarioRequirements["permission_flow"] = RequirementUnsupported
	}

	if providerType == "acp" {
		capabilities.ScenarioRequirements["permission_flow"] = RequirementRequired
		capabilities.ScenarioRequirements["mcp_integration"] = RequirementRequired
	}

	return capabilities
}

func classifyProviderStatus(scenarios []Scenario) string {
	seenPartial := false
	seenBlocked := false
	seenPass := false
	seenRunnable := false

	for _, scenario := range scenarios {
		switch scenario.Status {
		case ScenarioStatusFail:
			return ScenarioStatusFail
		case ScenarioStatusPartial:
			seenPartial = true
			seenRunnable = true
		case ScenarioStatusBlockedByConfig:
			seenBlocked = true
		case ScenarioStatusPass:
			seenPass = true
			seenRunnable = true
		case ScenarioStatusSkipped:
			// ignored
		default:
			seenPartial = true
		}
	}

	if seenPartial {
		return ScenarioStatusPartial
	}
	if seenBlocked && !seenRunnable {
		return ScenarioStatusBlockedByConfig
	}
	if seenBlocked && seenPass {
		return ScenarioStatusPartial
	}
	if seenPass {
		return ScenarioStatusPass
	}
	return ScenarioStatusSkipped
}
