package tools

// Filter returns the subset of defs permitted by the allowlist/denylist rules.
//
// Rules (applied in order):
//  1. If allowed is non-empty, only tools whose Name appears in allowed are kept.
//  2. Any tool whose Name appears in disallowed is removed (applied after the
//     allowlist, so it can remove entries that survived step 1).
//
// Empty slices mean "no restriction" for that list.
func Filter(defs []ToolDef, allowed, disallowed []string) []ToolDef {
	// Build lookup sets for O(1) membership tests.
	allowSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowSet[name] = struct{}{}
	}

	denySet := make(map[string]struct{}, len(disallowed))
	for _, name := range disallowed {
		denySet[name] = struct{}{}
	}

	out := make([]ToolDef, 0, len(defs))
	for _, def := range defs {
		// Step 1: allowlist filter (only when the list is non-empty).
		if len(allowed) > 0 {
			if _, ok := allowSet[def.Name]; !ok {
				continue
			}
		}

		// Step 2: denylist filter.
		if _, denied := denySet[def.Name]; denied {
			continue
		}

		out = append(out, def)
	}
	return out
}
