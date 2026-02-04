package pty

import (
	"regexp"
	"strings"
)

// StatusExtractor defines the interface for extracting agent status from terminal output.
type StatusExtractor interface {
	Extract(output string) (task string, err error)
}

// PositionExtractor extracts status from a specific line/column range.
type PositionExtractor struct {
	Row    int
	Col    int
	Length int
}

func (e *PositionExtractor) Extract(output string) (string, error) {
	lines := strings.Split(output, "\n")
	if e.Row < 0 || e.Row >= len(lines) {
		return "", nil
	}
	line := lines[e.Row]
	if e.Col < 0 || e.Col >= len(line) {
		return "", nil
	}
	end := e.Col + e.Length
	if end > len(line) {
		end = len(line)
	}
	return strings.TrimSpace(line[e.Col:end]), nil
}

// RegexExtractor extracts status using a regular expression with a named capture group "task".
type RegexExtractor struct {
	Regex *regexp.Regexp
}

func (e *RegexExtractor) Extract(output string) (string, error) {
	match := e.Regex.FindStringSubmatch(output)
	if match == nil {
		return "", nil
	}

	// Try named group "task" first
	for i, name := range e.Regex.SubexpNames() {
		if name == "task" && i < len(match) && match[i] != "" {
			return strings.TrimSpace(match[i]), nil
		}
	}

	// Fallback to first capture group if it exists
	if len(match) > 1 && match[1] != "" {
		return strings.TrimSpace(match[1]), nil
	}

	return strings.TrimSpace(match[0]), nil
}

// MultiExtractor tries multiple extractors in order until one returns a result.
type MultiExtractor struct {
	Extractors []StatusExtractor
}

func (e *MultiExtractor) Extract(output string) (string, error) {
	for _, ext := range e.Extractors {
		task, err := ext.Extract(output)
		if err == nil && task != "" {
			return task, nil
		}
	}
	return "", nil
}
