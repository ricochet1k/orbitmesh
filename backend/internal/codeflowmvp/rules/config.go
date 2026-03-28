package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RuleConfig is the top-level configuration for the CodeFlow analysis rules system.
// It encompasses Layer 1 (fact generation), Layer 2 (propagation and enrichment),
// and Layer 3 (analysis queries).
type RuleConfig struct {
	Version            string             `yaml:"version"`
	Rules              []FactRule         `yaml:"rules"`
	Propagation        *PropagationConfig `yaml:"propagation,omitempty"`
	Enrichment         []EnrichmentRule   `yaml:"enrichment,omitempty"`
	Analyses           []AnalysisRule     `yaml:"analyses,omitempty"`
	AnalysisTemplates  []AnalysisTemplate `yaml:"analysis_templates,omitempty"`
	Visuals            *VisualConfig      `yaml:"visuals,omitempty"`
}

// FactRule defines a Layer 1 fact generation rule that matches code patterns
// and produces graph nodes, edges, or semantic annotations.
type FactRule struct {
	ID               string              `yaml:"id"`
	Match            MatchSpec           `yaml:"match"`
	Node             *NodeSpec           `yaml:"node,omitempty"`
	Edge             *EdgeSpec           `yaml:"edge,omitempty"`
	ClosureSemantics *ClosureSemanticsSpec `yaml:"closure_semantics,omitempty"`
	ControlFlow      *ControlFlowSpec    `yaml:"control_flow,omitempty"`
	DataFlow         *DataFlowSpec       `yaml:"data_flow,omitempty"`
}

// MatchSpec describes how to match code elements for a rule.
type MatchSpec struct {
	Signature  string       `yaml:"signature,omitempty"`
	Kind       string       `yaml:"kind,omitempty"`
	Callee     []string     `yaml:"callee,omitempty"`
	TreeSitter string       `yaml:"tree_sitter,omitempty"`
	Context    *ContextSpec `yaml:"context,omitempty"`
	Package    string       `yaml:"package,omitempty"`
}

// ContextSpec constrains a match based on its AST context.
type ContextSpec struct {
	Ancestor *AncestorSpec `yaml:"ancestor,omitempty"`
}

// AncestorSpec specifies ancestor node constraints.
type AncestorSpec struct {
	Kind    []string `yaml:"kind,omitempty"`
	Pattern string   `yaml:"pattern,omitempty"`
}

// NodeSpec describes tags and properties to set on a matched graph node.
type NodeSpec struct {
	Tags              []string          `yaml:"tags,omitempty"`
	Properties        map[string]string `yaml:"properties,omitempty"`
	IdentityExpansion string            `yaml:"identity_expansion,omitempty"`
}

// EdgeSpec describes an edge to create in the graph.
type EdgeSpec struct {
	Type       string            `yaml:"type"`
	From       string            `yaml:"from"`
	To         string            `yaml:"to"`
	Properties map[string]string `yaml:"properties,omitempty"`
	Confidence float64           `yaml:"confidence,omitempty"`
}

// ClosureSemanticsSpec annotates how a closure parameter is executed.
type ClosureSemanticsSpec struct {
	Parameter string `yaml:"parameter"`
	Execution string `yaml:"execution"`
}

// ControlFlowSpec annotates control flow properties of a matched element.
type ControlFlowSpec struct {
	TerminatesExecution bool `yaml:"terminates_execution"`
}

// DataFlowSpec annotates data flow properties for taint tracking.
type DataFlowSpec struct {
	Role       string   `yaml:"role"`
	Categories []string `yaml:"categories,omitempty"`
	From       string   `yaml:"from,omitempty"`
	To         string   `yaml:"to,omitempty"`
}

// PropagationConfig controls Layer 2a tag propagation behavior.
type PropagationConfig struct {
	TagSets           []TagSet `yaml:"tag_sets,omitempty"`
	MaxDepth          int      `yaml:"max_depth,omitempty"`
	RespectBoundaries *bool    `yaml:"respect_boundaries,omitempty"`
	PropagateDomains  *bool    `yaml:"propagate_domains,omitempty"`
}

// TagSet defines a group of tags that propagate together.
type TagSet struct {
	ID            string   `yaml:"id"`
	Tags          []string `yaml:"tags"`
	PropagateOnly []string `yaml:"propagate_only,omitempty"`
}

// EnrichmentRule defines a Layer 2b graph enrichment pass using Cypher queries.
type EnrichmentRule struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description,omitempty"`
	Phase       int    `yaml:"phase"`
	Match       string `yaml:"match"`
	Write       string `yaml:"write"`
}

// AnalysisRule defines a Layer 3 analysis query that produces findings.
type AnalysisRule struct {
	ID          string            `yaml:"id"`
	Severity    string            `yaml:"severity"`
	Description string            `yaml:"description,omitempty"`
	Query       string            `yaml:"query,omitempty"`
	Explain     string            `yaml:"explain,omitempty"`
	Template    string            `yaml:"template,omitempty"`
	Params      map[string]string `yaml:"params,omitempty"`
}

// AnalysisTemplate defines a reusable parameterized analysis query.
type AnalysisTemplate struct {
	ID         string            `yaml:"id"`
	Parameters map[string]string `yaml:"parameters,omitempty"`
	Query      string            `yaml:"query"`
	Explain    string            `yaml:"explain,omitempty"`
}

// VisualConfig controls frontend presentation of graph elements.
type VisualConfig struct {
	Nodes   []VisualNodeRule `yaml:"nodes,omitempty"`
	Edges   []VisualEdgeRule `yaml:"edges,omitempty"`
	Queries []VisualQuery    `yaml:"queries,omitempty"`
}

// VisualNodeRule maps a tag to a visual node style.
type VisualNodeRule struct {
	MatchTag string `yaml:"match_tag"`
	Shape    string `yaml:"shape,omitempty"`
	Color    string `yaml:"color,omitempty"`
}

// VisualEdgeRule maps an edge type to a visual edge style.
type VisualEdgeRule struct {
	MatchType  string `yaml:"match_type"`
	StrokeDash string `yaml:"stroke_dash,omitempty"`
	Visible    *bool  `yaml:"visible,omitempty"`
}

// VisualQuery defines a named query with presentation hints.
type VisualQuery struct {
	ID           string              `yaml:"id"`
	Query        string              `yaml:"query"`
	Presentation *VisualPresentation `yaml:"presentation,omitempty"`
}

// VisualPresentation controls how query results are displayed.
type VisualPresentation struct {
	PathColor string `yaml:"path_color,omitempty"`
	Highlight bool   `yaml:"highlight,omitempty"`
}

// LoadConfig reads and parses a codeflow.yaml file.
func LoadConfig(path string) (*RuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return LoadConfigFromBytes(data)
}

// LoadConfigFromBytes parses YAML bytes into a RuleConfig.
func LoadConfigFromBytes(data []byte) (*RuleConfig, error) {
	var cfg RuleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	// Apply defaults for propagation config.
	if cfg.Propagation != nil {
		if cfg.Propagation.MaxDepth == 0 {
			cfg.Propagation.MaxDepth = 50
		}
		if cfg.Propagation.RespectBoundaries == nil {
			cfg.Propagation.RespectBoundaries = boolPtr(true)
		}
		if cfg.Propagation.PropagateDomains == nil {
			cfg.Propagation.PropagateDomains = boolPtr(true)
		}
	}
	return &cfg, nil
}

var validSeverities = map[string]bool{
	"critical": true,
	"high":     true,
	"medium":   true,
	"low":      true,
	"info":     true,
}

var validClosureExecutions = map[string]bool{
	"called_immediately": true,
	"spawned":            true,
	"stored":             true,
}

var validDataFlowRoles = map[string]bool{
	"source":     true,
	"sink":       true,
	"sanitizer":  true,
	"propagator": true,
}

// Validate checks the config for errors such as duplicate IDs, missing required
// fields, and invalid enum values.
func (c *RuleConfig) Validate() error {
	ids := make(map[string]bool)

	// Validate Layer 1 rules.
	for i, r := range c.Rules {
		if r.ID == "" {
			return fmt.Errorf("rules[%d]: missing required field 'id'", i)
		}
		if ids[r.ID] {
			return fmt.Errorf("rules[%d]: duplicate rule id %q", i, r.ID)
		}
		ids[r.ID] = true

		if err := validateMatchSpec(i, r.Match); err != nil {
			return err
		}

		if r.ClosureSemantics != nil {
			if !validClosureExecutions[r.ClosureSemantics.Execution] {
				return fmt.Errorf("rules[%d] %q: closure_semantics.execution must be one of: called_immediately, spawned, stored; got %q",
					i, r.ID, r.ClosureSemantics.Execution)
			}
		}

		if r.DataFlow != nil {
			if !validDataFlowRoles[r.DataFlow.Role] {
				return fmt.Errorf("rules[%d] %q: data_flow.role must be one of: source, sink, sanitizer, propagator; got %q",
					i, r.ID, r.DataFlow.Role)
			}
		}
	}

	// Validate Layer 2b enrichment rules.
	for i, e := range c.Enrichment {
		if e.ID == "" {
			return fmt.Errorf("enrichment[%d]: missing required field 'id'", i)
		}
		if ids[e.ID] {
			return fmt.Errorf("enrichment[%d]: duplicate rule id %q", i, e.ID)
		}
		ids[e.ID] = true

		if e.Phase <= 0 {
			return fmt.Errorf("enrichment[%d] %q: phase must be > 0; got %d", i, e.ID, e.Phase)
		}
	}

	// Validate Layer 3 analysis rules.
	for i, a := range c.Analyses {
		if a.ID == "" {
			return fmt.Errorf("analyses[%d]: missing required field 'id'", i)
		}
		if ids[a.ID] {
			return fmt.Errorf("analyses[%d]: duplicate rule id %q", i, a.ID)
		}
		ids[a.ID] = true

		if !validSeverities[strings.ToLower(a.Severity)] {
			return fmt.Errorf("analyses[%d] %q: severity must be one of: critical, high, medium, low, info; got %q",
				i, a.ID, a.Severity)
		}

		if a.Query == "" && a.Template == "" {
			return fmt.Errorf("analyses[%d] %q: must specify either 'query' or 'template'", i, a.ID)
		}
	}

	// Validate analysis templates.
	templateIDs := make(map[string]bool)
	for i, t := range c.AnalysisTemplates {
		if t.ID == "" {
			return fmt.Errorf("analysis_templates[%d]: missing required field 'id'", i)
		}
		if templateIDs[t.ID] {
			return fmt.Errorf("analysis_templates[%d]: duplicate template id %q", i, t.ID)
		}
		templateIDs[t.ID] = true

		if t.Query == "" {
			return fmt.Errorf("analysis_templates[%d] %q: missing required field 'query'", i, t.ID)
		}
	}

	// Validate propagation tag sets.
	if c.Propagation != nil {
		tagSetIDs := make(map[string]bool)
		for i, ts := range c.Propagation.TagSets {
			if ts.ID == "" {
				return fmt.Errorf("propagation.tag_sets[%d]: missing required field 'id'", i)
			}
			if tagSetIDs[ts.ID] {
				return fmt.Errorf("propagation.tag_sets[%d]: duplicate tag_set id %q", i, ts.ID)
			}
			tagSetIDs[ts.ID] = true
		}
	}

	return nil
}

// validateMatchSpec checks that at least one match field is provided.
func validateMatchSpec(ruleIdx int, m MatchSpec) error {
	if m.Signature == "" && m.Kind == "" && len(m.Callee) == 0 && m.TreeSitter == "" && m.Package == "" {
		return fmt.Errorf("rules[%d]: match must specify at least one of: signature, kind, callee, tree_sitter, package", ruleIdx)
	}
	return nil
}

// FindProjectConfig searches for codeflow.yaml starting from dir and walking up
// the directory tree. It looks for both "codeflow.yaml" and ".codeflow.yaml".
func FindProjectConfig(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	startDir := dir
	for {
		for _, name := range []string{"codeflow.yaml", ".codeflow.yaml"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", fmt.Errorf("no codeflow.yaml found in %s or any parent directory", startDir)
		}
		dir = parent
	}
}

func boolPtr(v bool) *bool { return &v }

// BoolVal dereferences a *bool, returning def if the pointer is nil.
func BoolVal(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}
