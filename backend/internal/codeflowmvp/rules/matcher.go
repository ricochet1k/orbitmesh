package rules

import (
	"fmt"
	"strings"
)

// MatchCallee checks if a callee expression matches any of the rule's callee patterns.
// Supports exact match and suffix match (e.g., "fetch" matches "window.fetch").
func MatchCallee(calleeExpr string, patterns []string) bool {
	for _, pattern := range patterns {
		if calleeExpr == pattern {
			return true
		}
		// Suffix match: pattern "fetch" matches "window.fetch".
		if strings.HasSuffix(calleeExpr, "."+pattern) {
			return true
		}
	}
	return false
}

// ParsedSignature represents a parsed signature pattern.
type ParsedSignature struct {
	Package  string   // e.g., "database/sql" or "*" or "**"
	Receiver string   // e.g., "*DB" or "*" or ""
	Method   string   // e.g., "Query" or "*"
	Params   []string // parameter patterns
}

// ParseSignature parses a signature string like "database/sql::*DB.Query(*)".
//
// Syntax:
//
//	Package::Receiver.Method(Args)  — full form
//	Package::Method(Args)           — function without receiver
//	::Method(Args)                  — any package
//	*  matches any single segment
//	** matches recursive packages
//	$param captures a parameter (for future use)
func ParseSignature(sig string) (*ParsedSignature, error) {
	if sig == "" {
		return nil, fmt.Errorf("empty signature")
	}

	parsed := &ParsedSignature{}

	// Split on "::" to separate package from the rest.
	parts := strings.SplitN(sig, "::", 2)
	var methodPart string
	if len(parts) == 2 {
		parsed.Package = parts[0]
		methodPart = parts[1]
	} else {
		// No "::" means the whole thing is the method part, any package.
		methodPart = parts[0]
	}

	// Extract parameters from parentheses if present.
	if idx := strings.Index(methodPart, "("); idx >= 0 {
		closeIdx := strings.LastIndex(methodPart, ")")
		if closeIdx < idx {
			return nil, fmt.Errorf("unmatched parentheses in signature %q", sig)
		}
		paramStr := methodPart[idx+1 : closeIdx]
		methodPart = methodPart[:idx]

		if paramStr != "" {
			params := strings.Split(paramStr, ",")
			for i := range params {
				params[i] = strings.TrimSpace(params[i])
			}
			parsed.Params = params
		}
	}

	// Split receiver and method on ".".
	if dotIdx := strings.LastIndex(methodPart, "."); dotIdx >= 0 {
		parsed.Receiver = methodPart[:dotIdx]
		parsed.Method = methodPart[dotIdx+1:]
	} else {
		parsed.Method = methodPart
	}

	if parsed.Method == "" {
		return nil, fmt.Errorf("empty method name in signature %q", sig)
	}

	return parsed, nil
}

// MatchSignature checks if a callee expression matches a parsed signature.
// Uses best-effort suffix matching since we don't have full type resolution yet.
func MatchSignature(calleeExpr string, sig *ParsedSignature) bool {
	if sig == nil {
		return false
	}

	// Extract the method name from the callee expression.
	// The callee might look like "db.Query", "sql.DB.Query", "Query", etc.
	exprParts := strings.Split(calleeExpr, ".")
	exprMethod := exprParts[len(exprParts)-1]

	// Strip trailing parenthesized args from the expression method if present.
	if idx := strings.Index(exprMethod, "("); idx >= 0 {
		exprMethod = exprMethod[:idx]
	}

	// Match method name.
	if sig.Method != "*" && sig.Method != exprMethod {
		return false
	}

	// If receiver is specified, try to match it.
	if sig.Receiver != "" && sig.Receiver != "*" {
		if len(exprParts) < 2 {
			// No receiver in expression but signature requires one.
			return false
		}
		exprReceiver := exprParts[len(exprParts)-2]
		if !matchSegment(exprReceiver, sig.Receiver) {
			return false
		}
	}

	// If package is specified, try best-effort match.
	if sig.Package != "" && sig.Package != "*" && sig.Package != "**" {
		// We don't always have the full package path in the callee expression,
		// so we use a heuristic: check if the callee expression contains a
		// prefix that could correspond to the package.
		if len(exprParts) > 2 {
			// The leading parts might represent the package.
			var pkgEnd int
			if sig.Receiver != "" {
				pkgEnd = len(exprParts) - 2
			} else {
				pkgEnd = len(exprParts) - 1
			}
			exprPkg := strings.Join(exprParts[:pkgEnd], "/")
			if !matchPackagePattern(exprPkg, sig.Package) {
				return false
			}
		}
		// If we only have method or receiver.method, we can't verify the
		// package, so we allow the match (best-effort).
	}

	return true
}

// matchSegment checks if an expression segment matches a pattern segment.
// "*" matches anything. Otherwise, exact match is required.
func matchSegment(expr, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return expr == pattern
}

// MatchKind checks if an AST node type matches a kind pattern.
// Supports exact match and wildcard "*" which matches anything.
func MatchKind(nodeKind string, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return nodeKind == pattern
}

// MatchPackage checks if a package path matches a glob pattern.
// Supports * (single segment) and ** (recursive).
func MatchPackage(pkgPath string, pattern string) bool {
	return matchPackagePattern(pkgPath, pattern)
}

// matchPackagePattern performs glob-style matching on package paths.
// "/" is the segment separator. "*" matches a single segment, "**" matches
// zero or more segments.
func matchPackagePattern(pkgPath string, pattern string) bool {
	if pattern == "**" {
		return true
	}
	if pattern == "*" {
		// Single wildcard matches a single-segment package.
		return !strings.Contains(pkgPath, "/")
	}

	patParts := strings.Split(pattern, "/")
	pkgParts := strings.Split(pkgPath, "/")

	return matchParts(pkgParts, patParts, 0, 0)
}

// matchParts recursively matches package path segments against pattern segments.
func matchParts(pkg, pat []string, pi, qi int) bool {
	for pi < len(pat) && qi < len(pkg) {
		if pat[pi] == "**" {
			// "**" matches zero or more segments.
			// Try matching the rest of the pattern against every possible
			// suffix of the package path.
			for k := qi; k <= len(pkg); k++ {
				if matchParts(pkg, pat, pi+1, k) {
					return true
				}
			}
			return false
		}
		if pat[pi] == "*" {
			// Matches any single segment.
			pi++
			qi++
			continue
		}
		if pat[pi] != pkg[qi] {
			return false
		}
		pi++
		qi++
	}

	// Consume trailing "**" patterns (they can match zero segments).
	for pi < len(pat) && pat[pi] == "**" {
		pi++
	}

	return pi == len(pat) && qi == len(pkg)
}

// MatchContext holds the context available when evaluating a rule match.
type MatchContext struct {
	CalleeExpr   string
	NodeKind     string
	PackageID    string
	InsideLoop   bool
	AncestorKinds []string // kinds of ancestor AST nodes
}

// EvaluateMatch checks if a MatchSpec matches the given context.
// A match requires ALL specified fields to match (AND semantics).
// Empty/unset fields are ignored (they match anything).
func EvaluateMatch(spec *MatchSpec, ctx *MatchContext) bool {
	if spec == nil || ctx == nil {
		return false
	}

	// Check callee patterns.
	if len(spec.Callee) > 0 {
		if ctx.CalleeExpr == "" || !MatchCallee(ctx.CalleeExpr, spec.Callee) {
			return false
		}
	}

	// Check signature.
	if spec.Signature != "" {
		sig, err := ParseSignature(spec.Signature)
		if err != nil {
			return false
		}
		if ctx.CalleeExpr == "" || !MatchSignature(ctx.CalleeExpr, sig) {
			return false
		}
	}

	// Check kind.
	if spec.Kind != "" {
		if ctx.NodeKind == "" || !MatchKind(ctx.NodeKind, spec.Kind) {
			return false
		}
	}

	// Check package.
	if spec.Package != "" {
		if ctx.PackageID == "" || !MatchPackage(ctx.PackageID, spec.Package) {
			return false
		}
	}

	// Check context constraints.
	if spec.Context != nil {
		if !matchContext(spec.Context, ctx) {
			return false
		}
	}

	return true
}

// matchContext evaluates ContextSpec constraints against the match context.
func matchContext(cs *ContextSpec, ctx *MatchContext) bool {
	if cs.Ancestor != nil {
		if !matchAncestor(cs.Ancestor, ctx) {
			return false
		}
	}
	return true
}

// matchAncestor checks if any ancestor kind matches the ancestor spec.
func matchAncestor(as *AncestorSpec, ctx *MatchContext) bool {
	if len(as.Kind) == 0 {
		return true
	}

	// At least one ancestor kind must match one of the required kinds.
	for _, required := range as.Kind {
		for _, ancestor := range ctx.AncestorKinds {
			if ancestor == required {
				return true
			}
		}
	}
	return false
}
