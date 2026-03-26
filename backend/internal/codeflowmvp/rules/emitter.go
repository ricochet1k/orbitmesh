package rules

import "strings"

// TagFact represents a tag to be applied to a node in the graph.
type TagFact struct {
	NodeID     string  `json:"node_id"`
	TagName    string  `json:"tag_name"`
	Domain     string  `json:"domain,omitempty"`
	RuleID     string  `json:"rule_id"`
	Source     string  `json:"source"` // "direct" or "propagated"
	Confidence float64 `json:"confidence"`
}

// EdgeFact represents an edge to be created between two nodes.
type EdgeFact struct {
	Type       string            `json:"type"`
	FromID     string            `json:"from_id"`
	ToID       string            `json:"to_id"`
	Properties map[string]string `json:"properties,omitempty"`
	RuleID     string            `json:"rule_id"`
	Confidence float64           `json:"confidence"`
}

// EmissionResult holds the tags and edges produced by evaluating rules.
type EmissionResult struct {
	Tags  []TagFact
	Edges []EdgeFact
}

// EmitFromRule produces tags and edges based on a matched rule.
// nodeID is the ID of the matched node (e.g., the call site ID).
// callerID is the ID of the enclosing function (if known).
func EmitFromRule(rule *FactRule, nodeID string, callerID string) EmissionResult {
	var result EmissionResult

	// Emit tags from the node spec.
	if rule.Node != nil && len(rule.Node.Tags) > 0 {
		domain := ""
		if rule.Node.Properties != nil {
			domain = rule.Node.Properties["domain"]
		}
		for _, tag := range rule.Node.Tags {
			result.Tags = append(result.Tags, TagFact{
				NodeID:     nodeID,
				TagName:    tag,
				Domain:     domain,
				RuleID:     rule.ID,
				Source:     "direct",
				Confidence: 1.0,
			})
		}
	}

	// Emit edge from the edge spec.
	if rule.Edge != nil {
		confidence := rule.Edge.Confidence
		if confidence == 0 {
			confidence = 1.0
		}

		fromID := resolveRef(rule.Edge.From, nodeID, callerID)
		toID := resolveRef(rule.Edge.To, nodeID, callerID)

		var props map[string]string
		if len(rule.Edge.Properties) > 0 {
			props = make(map[string]string, len(rule.Edge.Properties))
			for k, v := range rule.Edge.Properties {
				props[k] = v
			}
		}

		result.Edges = append(result.Edges, EdgeFact{
			Type:       rule.Edge.Type,
			FromID:     fromID,
			ToID:       toID,
			Properties: props,
			RuleID:     rule.ID,
			Confidence: confidence,
		})
	}

	return result
}

// resolveRef resolves a reference string to a concrete node ID.
// "self" resolves to nodeID, "enclosing_function" resolves to callerID.
// References starting with "$" are parameter placeholders and are returned
// as-is for future resolution. Any other value is returned unchanged.
func resolveRef(ref string, nodeID string, callerID string) string {
	switch ref {
	case "self":
		return nodeID
	case "enclosing_function":
		return callerID
	default:
		if strings.HasPrefix(ref, "$") {
			// Parameter placeholder -- return the literal for future resolution.
			return ref
		}
		return ref
	}
}
