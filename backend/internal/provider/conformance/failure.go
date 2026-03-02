package conformance

import (
	"fmt"
	"strings"
)

type FailureClassification string

const (
	FailureAuth               FailureClassification = "auth"
	FailureStartup            FailureClassification = "startup"
	FailureTransport          FailureClassification = "transport"
	FailureProtocolParse      FailureClassification = "protocol_parse"
	FailureEventNormalization FailureClassification = "event_normalization"
	FailurePermission         FailureClassification = "permission"
	FailureTool               FailureClassification = "tool"
	FailureMCP                FailureClassification = "mcp"
	FailureTimeout            FailureClassification = "timeout"
	FailureRateLimit          FailureClassification = "rate_limit"
)

type FailureReport struct {
	Classification FailureClassification `json:"classification"`
	Message        string                `json:"message"`
	Expected       string                `json:"expected"`
	Actual         string                `json:"actual"`
	RecentFrames   []string              `json:"recent_frames"`
}

func renderFailureMarkdown(report FailureReport) string {
	var b strings.Builder

	b.WriteString("# Conformance Failure\n\n")
	b.WriteString(fmt.Sprintf("- Classification: `%s`\n", report.Classification))
	if report.Message != "" {
		b.WriteString(fmt.Sprintf("- Message: %s\n", RedactString(report.Message)))
	}
	b.WriteString("\n")

	b.WriteString("## Expected\n\n")
	b.WriteString("```")
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(RedactString(report.Expected)))
	b.WriteString("\n```")
	b.WriteString("\n\n")

	b.WriteString("## Actual\n\n")
	b.WriteString("```")
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(RedactString(report.Actual)))
	b.WriteString("\n```")
	b.WriteString("\n\n")

	b.WriteString("## Recent Frames\n\n")
	if len(report.RecentFrames) == 0 {
		b.WriteString("No recent frames captured.\n")
		return b.String()
	}

	for _, frame := range report.RecentFrames {
		b.WriteString("- `")
		b.WriteString(strings.TrimSpace(RedactString(frame)))
		b.WriteString("`\n")
	}

	return b.String()
}
