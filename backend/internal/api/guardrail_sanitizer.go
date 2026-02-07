package api

import (
	"html"
	"regexp"
	"strings"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

var (
	guardrailHTMLTags     = regexp.MustCompile(`</?[^>]+(>|$)`)
	guardrailControlChars = regexp.MustCompile("[\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F\\x7F]")
	guardrailBearerToken  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*`)
	guardrailKeyValue     = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passphrase|access[_-]?key)\b\s*[:=]\s*([^\s,;]+)`)
	guardrailAWSAccessKey = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	guardrailGitHubToken  = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)
	guardrailWhitespace   = regexp.MustCompile(`\s+`)
)

const guardrailEntityDecodePasses = 3

func sanitizeGuardrailGuidance(input string) string {
	if input == "" {
		return ""
	}

	sanitized := decodeGuardrailEntities(input)
	sanitized = guardrailHTMLTags.ReplaceAllString(sanitized, "")
	sanitized = guardrailControlChars.ReplaceAllString(sanitized, " ")
	sanitized = guardrailBearerToken.ReplaceAllString(sanitized, "Bearer [redacted]")
	sanitized = guardrailKeyValue.ReplaceAllString(sanitized, "$1: [redacted]")
	sanitized = guardrailAWSAccessKey.ReplaceAllString(sanitized, "[redacted]")
	sanitized = guardrailGitHubToken.ReplaceAllString(sanitized, "[redacted]")
	sanitized = strings.TrimSpace(guardrailWhitespace.ReplaceAllString(sanitized, " "))
	return sanitized
}

func decodeGuardrailEntities(input string) string {
	decoded := input
	for i := 0; i < guardrailEntityDecodePasses; i++ {
		next := html.UnescapeString(decoded)
		if next == decoded {
			return decoded
		}
		decoded = next
	}
	return decoded
}

func sanitizeGuardrailStatus(guardrail apiTypes.GuardrailStatus) apiTypes.GuardrailStatus {
	guardrail.Detail = sanitizeGuardrailGuidance(guardrail.Detail)
	return guardrail
}

func sanitizePermissionsResponse(response apiTypes.PermissionsResponse) apiTypes.PermissionsResponse {
	if len(response.Guardrails) == 0 {
		return response
	}

	guardrails := make([]apiTypes.GuardrailStatus, len(response.Guardrails))
	for i, guardrail := range response.Guardrails {
		guardrails[i] = sanitizeGuardrailStatus(guardrail)
	}
	response.Guardrails = guardrails
	return response
}
