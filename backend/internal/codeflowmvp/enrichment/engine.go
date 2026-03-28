package enrichment

import (
	"fmt"
	"log"
	"sort"
)

// EnrichmentRule defines a graph enrichment rule.
type EnrichmentRule struct {
	ID          string `yaml:"id" json:"id"`
	Description string `yaml:"description" json:"description"`
	Phase       int    `yaml:"phase" json:"phase"`
	Match       string `yaml:"match" json:"match"` // Cypher query
	Write       string `yaml:"write" json:"write"` // Cypher mutation template
}

// GraphQuerier abstracts the graph database for enrichment queries.
type GraphQuerier interface {
	// Query executes a read-only Cypher query and returns results as maps.
	Query(cypher string) ([]map[string]interface{}, error)
	// Mutate executes a write Cypher query with the given parameters.
	Mutate(cypher string, params map[string]interface{}) error
}

// Engine runs enrichment rules against a graph.
type Engine struct {
	querier GraphQuerier
}

// NewEngine creates a new enrichment engine.
func NewEngine(querier GraphQuerier) *Engine {
	return &Engine{querier: querier}
}

// Run executes all enrichment rules in phase order.
// Rules in the same phase run sequentially. Phase N runs before phase N+1.
// Returns the number of enrichment operations performed.
func (eng *Engine) Run(rules []EnrichmentRule, scanEpoch string, producer string) (int, error) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Phase < rules[j].Phase
	})

	total := 0
	for _, rule := range rules {
		n, err := eng.RunRule(rule, scanEpoch, producer)
		if err != nil {
			log.Printf("enrichment rule %s failed: %v", rule.ID, err)
			continue
		}
		total += n
	}
	return total, nil
}

// RunRule executes a single enrichment rule.
// For each result from the match query, it executes the write query
// with the matched variables as parameters.
func (eng *Engine) RunRule(rule EnrichmentRule, scanEpoch string, producer string) (int, error) {
	rows, err := eng.querier.Query(rule.Match)
	if err != nil {
		return 0, fmt.Errorf("match query for rule %s failed: %w", rule.ID, err)
	}

	ops := 0
	for _, row := range rows {
		params := make(map[string]interface{}, len(row)+2)
		for k, v := range row {
			params[k] = v
		}
		params["scanEpoch"] = scanEpoch
		params["producer"] = producer

		if err := eng.querier.Mutate(rule.Write, params); err != nil {
			log.Printf("write query for rule %s failed on row %v: %v", rule.ID, row, err)
			continue
		}
		ops++
	}
	return ops, nil
}
