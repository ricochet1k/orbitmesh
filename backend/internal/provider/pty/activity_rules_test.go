package pty

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRuleConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	enabled := true
	top := 0
	bottom := 2
	config := &RuleConfig{
		Version: 1,
		Profiles: []RuleProfile{
			{
				ID:      "profile-1",
				Enabled: &enabled,
				Match: RuleProfileMatch{
					CommandRegex: "claude",
					ArgsRegex:    ".*",
				},
				Rules: []RuleDefinition{
					{
						ID:      "rule-1",
						Enabled: true,
						Trigger: RuleTrigger{RegionChanged: &RegionTrigger{Top: top, Bottom: bottom}},
						Extract: RuleExtract{
							Type:   "region_text",
							Region: RegionSpec{Top: &top, Bottom: &bottom},
						},
						Emit: RuleEmit{Kind: "agent_message"},
					},
				},
			},
		},
	}

	if err := SaveRuleConfig(path, config); err != nil {
		t.Fatalf("save rules config: %v", err)
	}

	loaded, err := LoadRuleConfig(path)
	if err != nil {
		t.Fatalf("load rules config: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected loaded config")
	}
	if loaded.Version != config.Version {
		t.Fatalf("version mismatch: got %d want %d", loaded.Version, config.Version)
	}
	if len(loaded.Profiles) != 1 {
		t.Fatalf("profiles length mismatch: got %d", len(loaded.Profiles))
	}
	profile := loaded.Profiles[0]
	if profile.ID != "profile-1" {
		t.Fatalf("profile id mismatch: got %s", profile.ID)
	}
	if profile.Enabled == nil || *profile.Enabled != enabled {
		t.Fatalf("profile enabled mismatch: got %v", profile.Enabled)
	}
	if len(profile.Rules) != 1 {
		t.Fatalf("rules length mismatch: got %d", len(profile.Rules))
	}
}

func TestSaveRuleConfigRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	config := &RuleConfig{Version: 2}
	if err := SaveRuleConfig(path, config); err == nil {
		t.Fatalf("expected error for invalid config")
	}
}
