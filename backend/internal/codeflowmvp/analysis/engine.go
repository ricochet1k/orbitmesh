package analysis

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"text/template"
)

// AnalysisRule defines an analysis query.
type AnalysisRule struct {
	ID          string            `yaml:"id" json:"id"`
	Severity    string            `yaml:"severity" json:"severity"`
	Description string            `yaml:"description" json:"description"`
	Query       string            `yaml:"query" json:"query"`
	Explain     string            `yaml:"explain" json:"explain"`
	Template    string            `yaml:"template,omitempty" json:"template,omitempty"`
	Params      map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
}

// AnalysisTemplate defines a reusable analysis template.
type AnalysisTemplate struct {
	ID         string            `yaml:"id" json:"id"`
	Parameters map[string]string `yaml:"parameters" json:"parameters"`
	Query      string            `yaml:"query" json:"query"`
	Explain    string            `yaml:"explain" json:"explain"`
}

// Finding represents a finding produced by an analysis query.
type Finding struct {
	RuleID      string  `json:"rule_id"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	NodeID      string  `json:"node_id,omitempty"`
	FileID      string  `json:"file_id,omitempty"`
	FunctionID  string  `json:"function_id,omitempty"`
	Fingerprint string  `json:"fingerprint"`
	Confidence  float64 `json:"confidence"`
}

// GraphReader abstracts read-only graph queries.
type GraphReader interface {
	Query(cypher string) ([]map[string]interface{}, error)
}

// Engine runs analysis queries and produces findings.
type Engine struct {
	reader    GraphReader
	templates map[string]*AnalysisTemplate
}

// NewEngine creates a new analysis engine.
func NewEngine(reader GraphReader, templates []AnalysisTemplate) *Engine {
	tmap := make(map[string]*AnalysisTemplate, len(templates))
	for i := range templates {
		tmap[templates[i].ID] = &templates[i]
	}
	return &Engine{
		reader:    reader,
		templates: tmap,
	}
}

// Run executes all analysis rules and returns findings.
func (eng *Engine) Run(rules []AnalysisRule) ([]Finding, error) {
	var all []Finding
	for _, rule := range rules {
		findings, err := eng.RunRule(rule)
		if err != nil {
			log.Printf("analysis rule %s failed: %v", rule.ID, err)
			continue
		}
		all = append(all, findings...)
	}
	return all, nil
}

// RunRule executes a single analysis rule and returns findings.
func (eng *Engine) RunRule(rule AnalysisRule) ([]Finding, error) {
	query := rule.Query
	explain := rule.Explain

	// If the rule references a template, resolve it.
	if rule.Template != "" {
		tmpl, ok := eng.templates[rule.Template]
		if !ok {
			return nil, fmt.Errorf("template %q not found for rule %s", rule.Template, rule.ID)
		}
		query = substituteParams(tmpl.Query, rule.Params)
		explain = substituteParams(tmpl.Explain, rule.Params)
	}

	rows, err := eng.reader.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query for rule %s failed: %w", rule.ID, err)
	}

	var findings []Finding
	for _, row := range rows {
		msg, err := renderExplain(explain, row)
		if err != nil {
			log.Printf("explain template render failed for rule %s: %v", rule.ID, err)
			msg = rule.Description
		}

		f := Finding{
			RuleID:      rule.ID,
			Severity:    rule.Severity,
			Message:     msg,
			NodeID:      extractStringField(row, "node_id", "fn", "req", "handler"),
			FileID:      extractStringField(row, "file_id"),
			FunctionID:  extractStringField(row, "function_id"),
			Fingerprint: computeFingerprint(rule.ID, row),
			Confidence:  0.8,
		}
		findings = append(findings, f)
	}
	return findings, nil
}

// substituteParams replaces template parameters in the form {{param}} with values from params.
func substituteParams(s string, params map[string]string) string {
	if len(params) == 0 {
		return s
	}
	pairs := make([]string, 0, len(params)*2)
	for k, v := range params {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// renderExplain renders the explain string as a Go text/template with row data.
func renderExplain(explain string, data map[string]interface{}) (string, error) {
	tmpl, err := template.New("explain").Parse(explain)
	if err != nil {
		return "", fmt.Errorf("parse explain template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute explain template: %w", err)
	}
	return buf.String(), nil
}

// computeFingerprint generates a stable fingerprint from the rule ID and key node IDs in the row.
func computeFingerprint(ruleID string, row map[string]interface{}) string {
	h := sha1.New()
	h.Write([]byte(ruleID))

	// Collect keys sorted for deterministic hashing.
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := row[k]
		// Check if the value is a map with an "id" field (node reference).
		if m, ok := v.(map[string]interface{}); ok {
			if id, exists := m["id"]; exists {
				h.Write([]byte(fmt.Sprintf("%s=%v", k, id)))
				continue
			}
		}
		// Include string values that look like IDs.
		if s, ok := v.(string); ok && (strings.HasSuffix(k, "_id") || k == "id") {
			h.Write([]byte(fmt.Sprintf("%s=%s", k, s)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// extractStringField looks through the row for the first matching key and returns its string value.
// For map values, it checks for an "id" sub-field first, then "file_id".
func extractStringField(row map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok {
			return s
		}
		if m, ok := v.(map[string]interface{}); ok {
			if id, exists := m["id"]; exists {
				return fmt.Sprintf("%v", id)
			}
			if fid, exists := m["file_id"]; exists {
				return fmt.Sprintf("%v", fid)
			}
		}
	}
	return ""
}
