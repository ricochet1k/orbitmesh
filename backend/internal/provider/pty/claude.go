package pty

import (
	"regexp"
)

var claudeTaskRegex = regexp.MustCompile(`(?m)^Task: (.*)$`)

func NewClaudePTYProvider(sessionID string) *PTYProvider {
	extractor := &RegexExtractor{
		Regex: claudeTaskRegex,
	}
	return NewPTYProvider(sessionID, extractor)
}
