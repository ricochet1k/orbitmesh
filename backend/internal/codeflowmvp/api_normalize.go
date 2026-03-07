package codeflowmvp

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	placeholderSegmentPattern = regexp.MustCompile(`^(:[^/]+|\{[^}]+\}|\$\{[^}]+\})$`)
	templateExprPattern       = regexp.MustCompile(`\$\{[^}]+\}`)
	trailingIDPattern         = regexp.MustCompile(`^[0-9a-fA-F-]{6,}$`)
	identifierTailPattern     = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*$`)
)

func normalizeHTTPMethod(raw string) string {
	method := strings.ToUpper(strings.TrimSpace(raw))
	if method == "" {
		return "GET"
	}
	return method
}

func normalizeAPIPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	raw = templateExprPattern.ReplaceAllString(raw, "{param}")
	raw = stripQuoted(raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if parsed, err := url.Parse(raw); err == nil {
		if parsed.Path != "" {
			raw = parsed.Path
		} else if parsed.Opaque != "" {
			raw = parsed.Opaque
		}
	}

	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}

	cleaned := path.Clean(raw)
	if cleaned == "." {
		cleaned = "/"
	}

	parts := strings.Split(cleaned, "/")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if placeholderSegmentPattern.MatchString(part) || trailingIDPattern.MatchString(part) {
			normalized = append(normalized, "{param}")
			continue
		}
		normalized = append(normalized, strings.ToLower(part))
	}

	if len(normalized) == 0 {
		return "/"
	}
	return "/" + strings.Join(normalized, "/")
}

func pathJoinNormalized(base string, route string) string {
	base = normalizeAPIPath(base)
	route = strings.TrimSpace(route)
	if route == "" {
		return base
	}
	if strings.HasPrefix(route, "/") {
		return normalizeAPIPath(route)
	}
	if base == "" {
		base = "/"
	}
	return normalizeAPIPath(path.Join(base, route))
}

func stripQuoted(raw string) string {
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') || (raw[0] == '`' && raw[len(raw)-1] == '`') {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func parseStringLiteral(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", false
	}
	if raw[0] == '`' && raw[len(raw)-1] == '`' {
		return templateExprPattern.ReplaceAllString(raw[1:len(raw)-1], "{param}"), true
	}
	if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
		parsed, err := strconv.Unquote(raw)
		if err == nil {
			return parsed, true
		}
		return raw[1 : len(raw)-1], true
	}
	return "", false
}

func tailIdentifier(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return ""
	}
	match := identifierTailPattern.FindString(trimmed)
	return match
}
