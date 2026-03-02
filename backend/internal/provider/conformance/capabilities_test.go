package conformance

import "testing"

func TestCapabilitiesForProvider_IgnoresADKAndPTY(t *testing.T) {
	t.Parallel()

	adk := capabilitiesForProvider("adk")
	if !adk.Ignored {
		t.Fatalf("adk ignored = %v, want true", adk.Ignored)
	}
	pty := capabilitiesForProvider("pty")
	if !pty.Ignored {
		t.Fatalf("pty ignored = %v, want true", pty.Ignored)
	}
}

func TestCapabilitiesForProvider_RequirementOverrides(t *testing.T) {
	t.Parallel()

	claudeWS := capabilitiesForProvider("claude-ws")
	claude := capabilitiesForProvider("claude")
	if claude.ScenarioRequirements["reasoning_progress"] != RequirementOptional {
		t.Fatalf("claude reasoning requirement = %s, want optional", claude.ScenarioRequirements["reasoning_progress"])
	}
	if claudeWS.ScenarioRequirements["permission_flow"] != RequirementUnsupported {
		t.Fatalf("claude-ws permission requirement = %s, want unsupported", claudeWS.ScenarioRequirements["permission_flow"])
	}
	if claudeWS.ScenarioRequirements["mcp_integration"] != RequirementUnsupported {
		t.Fatalf("claude-ws mcp requirement = %s, want unsupported", claudeWS.ScenarioRequirements["mcp_integration"])
	}

	openai := capabilitiesForProvider("openai")
	if openai.ScenarioRequirements["permission_flow"] != RequirementUnsupported {
		t.Fatalf("openai permission requirement = %s, want unsupported", openai.ScenarioRequirements["permission_flow"])
	}
}

func TestClassifyProviderStatus_Buckets(t *testing.T) {
	t.Parallel()

	if got := classifyProviderStatus([]Scenario{{Status: ScenarioStatusPass}}); got != ScenarioStatusPass {
		t.Fatalf("status = %s, want pass", got)
	}
	if got := classifyProviderStatus([]Scenario{{Status: ScenarioStatusBlockedByConfig}}); got != ScenarioStatusBlockedByConfig {
		t.Fatalf("status = %s, want blocked_by_config", got)
	}
	if got := classifyProviderStatus([]Scenario{{Status: ScenarioStatusPass}, {Status: ScenarioStatusBlockedByConfig}}); got != ScenarioStatusPartial {
		t.Fatalf("status = %s, want partial", got)
	}
	if got := classifyProviderStatus([]Scenario{{Status: ScenarioStatusPartial}}); got != ScenarioStatusPartial {
		t.Fatalf("status = %s, want partial", got)
	}
	if got := classifyProviderStatus([]Scenario{{Status: ScenarioStatusFail}}); got != ScenarioStatusFail {
		t.Fatalf("status = %s, want fail", got)
	}
}
