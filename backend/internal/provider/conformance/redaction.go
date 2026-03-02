package conformance

import (
	"regexp"
	"strings"
)

var (
	bearerTokenPattern    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-+/=]+`)
	authorizationPattern  = regexp.MustCompile(`(?i)("?authorization"?\s*[:=]\s*"?)([^"\n\r,}]+)("?)`)
	apiKeyPattern         = regexp.MustCompile(`(?i)("?(?:x-api-key|api[_-]?key)"?\s*[:=]\s*"?)([^"\n\r,}]+)("?)`)
	genericSecretPattern  = regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|token|secret|password)"?\s*[:=]\s*"?)([^"\n\r,}]+)("?)`)
	headersSensitiveNames = map[string]struct{}{
		"authorization": {},
		"x-api-key":     {},
	}
)

func RedactString(input string) string {
	output := bearerTokenPattern.ReplaceAllString(input, "Bearer [REDACTED]")
	output = authorizationPattern.ReplaceAllString(output, `${1}[REDACTED]${3}`)
	output = apiKeyPattern.ReplaceAllString(output, `${1}[REDACTED]${3}`)
	output = genericSecretPattern.ReplaceAllString(output, `${1}[REDACTED]${3}`)
	return output
}

func RedactHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	redacted := make(map[string]string, len(headers))
	for key, value := range headers {
		_, sensitive := headersSensitiveNames[strings.ToLower(key)]
		if sensitive {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "bearer ") {
				redacted[key] = "Bearer [REDACTED]"
			} else {
				redacted[key] = "[REDACTED]"
			}
			continue
		}

		redacted[key] = RedactString(value)
	}

	return redacted
}
