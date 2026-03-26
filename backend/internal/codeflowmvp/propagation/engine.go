package propagation

import "container/list"

// PropagationConfig configures the tag propagation engine.
type PropagationConfig struct {
	TagSets           []TagSet `yaml:"tag_sets" json:"tag_sets"`
	MaxDepth          int      `yaml:"max_depth" json:"max_depth"`
	RespectBoundaries *bool    `yaml:"respect_boundaries" json:"respect_boundaries"`
	PropagateDomains  *bool    `yaml:"propagate_domains" json:"propagate_domains"`
}

// TagSet defines a group of tags and optional propagation constraints.
type TagSet struct {
	ID            string   `yaml:"id" json:"id"`
	Tags          []string `yaml:"tags" json:"tags"`
	PropagateOnly []string `yaml:"propagate_only,omitempty" json:"propagate_only,omitempty"`
}

// Tag represents a tag assigned to a node.
type Tag struct {
	NodeID         string  `json:"node_id"`
	Name           string  `json:"name"`
	Domain         string  `json:"domain,omitempty"`
	Source         string  `json:"source"` // "direct" or "propagated"
	RuleID         string  `json:"rule_id,omitempty"`
	PropagatedFrom string  `json:"propagated_from,omitempty"`
	Confidence     float64 `json:"confidence"`
}

// CallEdge represents a call from one function to another.
type CallEdge struct {
	CallerID string
	CalleeID string
}

// FunctionInfo holds metadata about a function relevant to propagation.
type FunctionInfo struct {
	ID          string
	IsBoundary  bool
	ClosureExec string // "called_immediately", "spawned", "stored", or ""
}

// PropagationInput is the input to the propagation engine.
type PropagationInput struct {
	Functions  []FunctionInfo
	CallEdges  []CallEdge
	DirectTags []Tag // Tags from Layer 1 (source="direct")
	Config     PropagationConfig
}

// PropagationResult is the output of the propagation engine.
type PropagationResult struct {
	PropagatedTags  []Tag // New tags with source="propagated"
	TotalDirect     int
	TotalPropagated int
}

// DefaultConfig returns a PropagationConfig with sensible defaults.
func DefaultConfig() PropagationConfig {
	t, f := true, false
	return PropagationConfig{
		MaxDepth:          50,
		RespectBoundaries: &t,
		PropagateDomains:  &f,
	}
}

// boolVal dereferences a *bool, returning def if the pointer is nil.
func boolVal(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

// tagKey uniquely identifies a tag assignment on a node.
type tagKey struct {
	nodeID  string
	tagName string
	domain  string
}

// workItem tracks a node to process along with its current propagation depth.
type workItem struct {
	nodeID string
	depth  int
}

// Propagate runs the tag propagation algorithm.
//
// The algorithm flows effect tags upward through the call graph:
//  1. Build reverse call graph: for each function, who calls it?
//  2. Collect all nodes with direct tags.
//  3. For each tag_set, propagate eligible tags through callers,
//     respecting boundary and closure semantics.
//  4. Return all newly propagated tags.
func Propagate(input PropagationInput) PropagationResult {
	maxDepth := input.Config.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 50
	}

	respectBoundaries := boolVal(input.Config.RespectBoundaries, true)

	// Index functions by ID.
	funcByID := make(map[string]*FunctionInfo, len(input.Functions))
	for i := range input.Functions {
		f := &input.Functions[i]
		funcByID[f.ID] = f
	}

	// Build reverse call graph: calleeID -> list of (callerID, closure semantics).
	type callerInfo struct {
		callerID    string
		closureExec string
	}
	reverseGraph := make(map[string][]callerInfo)
	for _, edge := range input.CallEdges {
		// The closure semantics come from the callee function info, representing
		// how the callee is invoked in the context of the call.
		calleeFunc := funcByID[edge.CalleeID]
		closureExec := ""
		if calleeFunc != nil {
			closureExec = calleeFunc.ClosureExec
		}
		reverseGraph[edge.CalleeID] = append(reverseGraph[edge.CalleeID], callerInfo{
			callerID:    edge.CallerID,
			closureExec: closureExec,
		})
	}

	// Index direct tags by nodeID.
	directTagsByNode := make(map[string][]Tag)
	for _, t := range input.DirectTags {
		directTagsByNode[t.NodeID] = append(directTagsByNode[t.NodeID], t)
	}

	// Track all existing tag assignments to avoid duplicates.
	existing := make(map[tagKey]bool)
	for _, t := range input.DirectTags {
		existing[tagKey{nodeID: t.NodeID, tagName: t.Name, domain: t.Domain}] = true
	}

	var propagatedTags []Tag

	// If no tag sets are configured, propagate all direct tags without filtering.
	tagSets := input.Config.TagSets
	if len(tagSets) == 0 {
		tagSets = []TagSet{{ID: "_default"}}
	}

	for _, ts := range tagSets {
		// Determine which tag names to propagate for this tag set.
		propagateSet := buildPropagateSet(ts)

		// Seed the worklist with all directly-tagged nodes that have eligible tags.
		// Map from nodeID to the tags that should propagate from it.
		type seedTag struct {
			tag  Tag
			from string // the originating nodeID for propagated_from
		}

		worklist := list.New()
		// nodeTags tracks which tags are pending propagation from each node.
		nodeTags := make(map[string][]seedTag)

		for nodeID, tags := range directTagsByNode {
			for _, t := range tags {
				if propagateSet != nil && !propagateSet[t.Name] {
					continue
				}
				key := nodeID
				nodeTags[key] = append(nodeTags[key], seedTag{tag: t, from: nodeID})
			}
		}

		// Initialize worklist with all seed nodes.
		queued := make(map[string]bool)
		for nodeID := range nodeTags {
			worklist.PushBack(workItem{nodeID: nodeID, depth: 0})
			queued[nodeID] = true
		}

		// Process worklist.
		for worklist.Len() > 0 {
			front := worklist.Front()
			item := front.Value.(workItem)
			worklist.Remove(front)

			if item.depth >= maxDepth {
				continue
			}

			tags := nodeTags[item.nodeID]
			if len(tags) == 0 {
				continue
			}

			callers := reverseGraph[item.nodeID]
			for _, ci := range callers {
				callerFunc := funcByID[ci.callerID]

				// Boundary check: if caller is a boundary, propagation halts.
				if respectBoundaries && callerFunc != nil && callerFunc.IsBoundary {
					continue
				}

				// Closure semantics check.
				switch ci.closureExec {
				case "spawned", "stored":
					continue
				}
				// "called_immediately" or "": propagate.

				var addedNew bool
				for _, st := range tags {
					domain := ""
					if boolVal(input.Config.PropagateDomains, false) {
						domain = st.tag.Domain
					}

					key := tagKey{
						nodeID:  ci.callerID,
						tagName: st.tag.Name,
						domain:  domain,
					}
					if existing[key] {
						continue
					}
					existing[key] = true

					newTag := Tag{
						NodeID:         ci.callerID,
						Name:           st.tag.Name,
						Domain:         domain,
						Source:         "propagated",
						RuleID:         st.tag.RuleID,
						PropagatedFrom: st.from,
						Confidence:     st.tag.Confidence * 0.9,
					}
					propagatedTags = append(propagatedTags, newTag)

					// Track that this caller now carries this tag for further propagation.
					nodeTags[ci.callerID] = append(nodeTags[ci.callerID], seedTag{
						tag:  newTag,
						from: st.from,
					})
					addedNew = true
				}

				if addedNew && !queued[ci.callerID] {
					worklist.PushBack(workItem{
						nodeID: ci.callerID,
						depth:  item.depth + 1,
					})
					queued[ci.callerID] = true
				}
			}
		}
	}

	return PropagationResult{
		PropagatedTags:  propagatedTags,
		TotalDirect:     len(input.DirectTags),
		TotalPropagated: len(propagatedTags),
	}
}

// buildPropagateSet returns the set of tag names eligible for propagation
// within a tag set. Returns nil if all tags should propagate (no filtering).
func buildPropagateSet(ts TagSet) map[string]bool {
	if len(ts.PropagateOnly) > 0 {
		m := make(map[string]bool, len(ts.PropagateOnly))
		for _, t := range ts.PropagateOnly {
			m[t] = true
		}
		return m
	}
	if len(ts.Tags) > 0 {
		m := make(map[string]bool, len(ts.Tags))
		for _, t := range ts.Tags {
			m[t] = true
		}
		return m
	}
	// No filtering: propagate all tags.
	return nil
}
