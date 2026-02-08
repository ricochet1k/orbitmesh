package pty

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

const ptyRulesConfigName = "pty-rules.v1.json"

type RuleConfig struct {
	Version  int           `json:"version"`
	Profiles []RuleProfile `json:"profiles"`
}

type RuleProfile struct {
	ID    string           `json:"id"`
	Match RuleProfileMatch `json:"match"`
	Rules []RuleDefinition `json:"rules"`
}

type RuleProfileMatch struct {
	CommandRegex string `json:"command_regex"`
	ArgsRegex    string `json:"args_regex"`
}

type RuleDefinition struct {
	ID       string        `json:"id"`
	Enabled  bool          `json:"enabled"`
	Trigger  RuleTrigger   `json:"trigger"`
	Extract  RuleExtract   `json:"extract"`
	Emit     RuleEmit      `json:"emit"`
	Identity *RuleIdentity `json:"identity,omitempty"`
}

type RuleIdentity struct {
	Capture string `json:"capture,omitempty"`
	Static  string `json:"static,omitempty"`
}

type RuleTrigger struct {
	RegionChanged *RegionTrigger `json:"region_changed,omitempty"`
}

type RegionTrigger struct {
	Top    int  `json:"top"`
	Bottom int  `json:"bottom"`
	Left   *int `json:"left,omitempty"`
	Right  *int `json:"right,omitempty"`
}

type RuleExtract struct {
	Type    string     `json:"type"`
	Region  RegionSpec `json:"region"`
	Pattern string     `json:"pattern,omitempty"`
}

type RegionSpec struct {
	Top    *int `json:"top"`
	Bottom *int `json:"bottom"`
	Left   *int `json:"left,omitempty"`
	Right  *int `json:"right,omitempty"`
}

type RuleEmit struct {
	Kind         string `json:"kind"`
	UpdateWindow string `json:"update_window,omitempty"`
	Finalize     bool   `json:"finalize,omitempty"`
	Open         *bool  `json:"open,omitempty"`
}

type CompiledProfile struct {
	ID          string
	CommandExpr *regexp.Regexp
	ArgsExpr    *regexp.Regexp
	Rules       []CompiledRule
}

type CompiledRule struct {
	ID       string
	Enabled  bool
	Trigger  RegionTrigger
	Extract  RuleExtract
	Emit     RuleEmit
	Regex    *regexp.Regexp
	Identity RuleIdentity
}

func DefaultRulesPath() string {
	return filepath.Join(storage.DefaultBaseDir(), "extractors", ptyRulesConfigName)
}

func LoadRuleConfig(path string) (*RuleConfig, error) {
	if path == "" {
		path = DefaultRulesPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rules config: %w", err)
	}
	var cfg RuleConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse rules config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *RuleConfig) Validate() error {
	if c == nil {
		return errors.New("rules config is nil")
	}
	if c.Version != 1 {
		return fmt.Errorf("rules config version must be 1, got %d", c.Version)
	}
	if len(c.Profiles) == 0 {
		return errors.New("rules config must include at least one profile")
	}
	seenProfiles := make(map[string]struct{})
	for _, profile := range c.Profiles {
		if strings.TrimSpace(profile.ID) == "" {
			return errors.New("profile id is required")
		}
		if _, ok := seenProfiles[profile.ID]; ok {
			return fmt.Errorf("duplicate profile id %q", profile.ID)
		}
		seenProfiles[profile.ID] = struct{}{}
		if err := validateProfile(profile); err != nil {
			return fmt.Errorf("profile %s: %w", profile.ID, err)
		}
	}
	return nil
}

func (c *RuleConfig) MatchProfile(command string, args []string) (*CompiledProfile, error) {
	if c == nil {
		return nil, errors.New("rules config is nil")
	}
	for _, profile := range c.Profiles {
		compiled, err := CompileProfile(profile)
		if err != nil {
			return nil, err
		}
		if matchProfile(compiled, command, args) {
			return compiled, nil
		}
	}
	return nil, nil
}

func CompileProfile(profile RuleProfile) (*CompiledProfile, error) {
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	compiled := &CompiledProfile{ID: profile.ID}
	if profile.Match.CommandRegex != "" {
		expr, err := regexp.Compile(profile.Match.CommandRegex)
		if err != nil {
			return nil, fmt.Errorf("profile %s command_regex: %w", profile.ID, err)
		}
		compiled.CommandExpr = expr
	}
	if profile.Match.ArgsRegex != "" {
		expr, err := regexp.Compile(profile.Match.ArgsRegex)
		if err != nil {
			return nil, fmt.Errorf("profile %s args_regex: %w", profile.ID, err)
		}
		compiled.ArgsExpr = expr
	}

	compiled.Rules = make([]CompiledRule, 0, len(profile.Rules))
	for _, rule := range profile.Rules {
		compiledRule, err := compileRule(profile.ID, rule)
		if err != nil {
			return nil, err
		}
		compiled.Rules = append(compiled.Rules, compiledRule)
	}
	return compiled, nil
}

func matchProfile(profile *CompiledProfile, command string, args []string) bool {
	if profile == nil {
		return false
	}
	if profile.CommandExpr != nil && !profile.CommandExpr.MatchString(command) {
		return false
	}
	if profile.ArgsExpr != nil {
		argsString := strings.Join(args, " ")
		if !profile.ArgsExpr.MatchString(argsString) {
			return false
		}
	}
	return true
}

func validateProfile(profile RuleProfile) error {
	if strings.TrimSpace(profile.ID) == "" {
		return errors.New("profile id is required")
	}
	if len(profile.Rules) == 0 {
		return errors.New("profile must include at least one rule")
	}
	seenRules := make(map[string]struct{})
	for _, rule := range profile.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return errors.New("rule id is required")
		}
		if _, ok := seenRules[rule.ID]; ok {
			return fmt.Errorf("duplicate rule id %q", rule.ID)
		}
		seenRules[rule.ID] = struct{}{}
		if err := validateRule(profile.ID, rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(profileID string, rule RuleDefinition) error {
	if rule.Trigger.RegionChanged == nil {
		return fmt.Errorf("rule %s missing trigger", rule.ID)
	}
	if err := validateTrigger(rule.Trigger.RegionChanged); err != nil {
		return fmt.Errorf("rule %s trigger: %w", rule.ID, err)
	}
	if err := validateExtract(rule.Extract); err != nil {
		return fmt.Errorf("rule %s extract: %w", rule.ID, err)
	}
	if strings.TrimSpace(rule.Emit.Kind) == "" {
		return fmt.Errorf("rule %s emit.kind is required", rule.ID)
	}
	if rule.Emit.UpdateWindow != "" && rule.Emit.UpdateWindow != "recent_open" {
		return fmt.Errorf("rule %s emit.update_window must be recent_open", rule.ID)
	}
	return nil
}

func validateTrigger(trigger *RegionTrigger) error {
	if trigger.Bottom <= trigger.Top {
		return fmt.Errorf("bottom must be greater than top")
	}
	if trigger.Top < 0 {
		return fmt.Errorf("top must be >= 0")
	}
	if trigger.Left != nil && trigger.Right != nil && *trigger.Right <= *trigger.Left {
		return fmt.Errorf("right must be greater than left")
	}
	return nil
}

func validateExtract(extract RuleExtract) error {
	if extract.Type != "region_text" && extract.Type != "region_regex" {
		return fmt.Errorf("unsupported extract type %q", extract.Type)
	}
	if extract.Type == "region_regex" && strings.TrimSpace(extract.Pattern) == "" {
		return fmt.Errorf("pattern is required for region_regex")
	}
	if extract.Region.Top == nil || extract.Region.Bottom == nil {
		return fmt.Errorf("region top and bottom are required")
	}
	if *extract.Region.Bottom <= *extract.Region.Top {
		return fmt.Errorf("region bottom must be greater than top")
	}
	if *extract.Region.Top < 0 {
		return fmt.Errorf("region top must be >= 0")
	}
	if extract.Region.Left != nil && extract.Region.Right != nil && *extract.Region.Right <= *extract.Region.Left {
		return fmt.Errorf("region right must be greater than left")
	}
	return nil
}

func compileRule(profileID string, rule RuleDefinition) (CompiledRule, error) {
	if err := validateRule(profileID, rule); err != nil {
		return CompiledRule{}, err
	}
	compiled := CompiledRule{
		ID:      rule.ID,
		Enabled: rule.Enabled,
		Extract: rule.Extract,
		Emit:    rule.Emit,
	}
	if rule.Trigger.RegionChanged != nil {
		compiled.Trigger = *rule.Trigger.RegionChanged
	}
	if rule.Identity != nil {
		compiled.Identity = *rule.Identity
	}
	if rule.Extract.Type == "region_regex" {
		expr, err := regexp.Compile(rule.Extract.Pattern)
		if err != nil {
			return CompiledRule{}, fmt.Errorf("rule %s regex: %w", rule.ID, err)
		}
		compiled.Regex = expr
	}
	return compiled, nil
}
