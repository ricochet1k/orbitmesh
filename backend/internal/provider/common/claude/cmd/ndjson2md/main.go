package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ndjson2md <file.ndjson>")
		os.Exit(1)
	}

	file, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large lines
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	var currentToolUse *ToolUse
	var currentTextBlock strings.Builder

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		// Parse the NDJSON wrapper
		var wrapper struct {
			Type    string          `json:"type"`
			Subtype string          `json:"subtype"`
			Event   json.RawMessage `json:"event"`
			Message json.RawMessage `json:"message"`
		}

		if err := json.Unmarshal(line, &wrapper); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to parse line %d: %v\n", lineNum, err)
			continue
		}

		switch wrapper.Type {
		case "system":
			handleSystemEvent(line)

		case "stream_event":
			handleStreamEvent(wrapper.Event, &currentTextBlock, &currentToolUse)

		case "assistant":
			// These are periodic snapshots, we can skip them since we process stream_events
			continue

		case "user":
			handleUserEvent(line)

		default:
			// Silently skip unknown types
			continue
		}
	}

	// Flush any remaining text
	if currentTextBlock.Len() > 0 {
		fmt.Print(currentTextBlock.String())
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
}

type ToolUse struct {
	Name  string
	ID    string
	Input strings.Builder
}

func handleSystemEvent(line []byte) {
	var init struct {
		Type              string   `json:"type"`
		Subtype           string   `json:"subtype"`
		CWD               string   `json:"cwd"`
		SessionID         string   `json:"session_id"`
		Model             string   `json:"model"`
		PermissionMode    string   `json:"permissionMode"`
		ClaudeCodeVersion string   `json:"claude_code_version"`
		Tools             []string `json:"tools"`
	}

	if err := json.Unmarshal(line, &init); err != nil {
		return
	}

	if init.Subtype == "init" {
		fmt.Printf("# Claude Code Session\n\n")
		fmt.Printf("**Session ID:** `%s`\n\n", init.SessionID)
		fmt.Printf("**Model:** `%s`\n\n", init.Model)
		fmt.Printf("**Working Directory:** `%s`\n\n", init.CWD)
		fmt.Printf("**Version:** `%s`\n\n", init.ClaudeCodeVersion)
		fmt.Printf("**Permission Mode:** `%s`\n\n", init.PermissionMode)
		fmt.Println("---\n")
	}
}

func handleUserEvent(line []byte) {
	var userMsg struct {
		Type           string `json:"type"`
		Message        struct {
			Role    string `json:"role"`
			Content []struct {
				Type       string `json:"type"`
				ToolUseID  string `json:"tool_use_id"`
				Content    string `json:"content"`
				IsError    bool   `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
		ToolUseResult struct {
			Stdout  string `json:"stdout"`
			Stderr  string `json:"stderr"`
		} `json:"tool_use_result"`
	}

	if err := json.Unmarshal(line, &userMsg); err != nil {
		return
	}

	// Tool results - show in collapsed section to avoid clutter
	if len(userMsg.Message.Content) > 0 {
		for _, content := range userMsg.Message.Content {
			if content.Type == "tool_result" {
				// Skip tool results for now - they clutter the output
				// If you want to see them, uncomment below:
				// fmt.Printf("<details><summary>Tool Result</summary>\n\n")
				// fmt.Printf("```\n%s\n```\n\n", truncate(content.Content, 500))
				// fmt.Printf("</details>\n\n")
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

func handleStreamEvent(eventData json.RawMessage, currentTextBlock *strings.Builder, currentToolUse **ToolUse) {
	var event struct {
		Type         string          `json:"type"`
		Index        int             `json:"index"`
		ContentBlock json.RawMessage `json:"content_block"`
		Delta        json.RawMessage `json:"delta"`
	}

	if err := json.Unmarshal(eventData, &event); err != nil {
		return
	}

	switch event.Type {
	case "message_start":
		// New message starting
		if currentTextBlock.Len() > 0 {
			fmt.Print(currentTextBlock.String())
			fmt.Println()
			currentTextBlock.Reset()
		}

	case "content_block_start":
		var contentBlock struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			ID    string          `json:"id"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(event.ContentBlock, &contentBlock)

		if contentBlock.Type == "text" {
			// Starting a new text block
			if currentTextBlock.Len() > 0 {
				fmt.Print(currentTextBlock.String())
				fmt.Println()
				currentTextBlock.Reset()
			}
		} else if contentBlock.Type == "tool_use" {
			// Starting a tool use block
			if currentTextBlock.Len() > 0 {
				fmt.Print(currentTextBlock.String())
				fmt.Println("\n")
				currentTextBlock.Reset()
			}
			*currentToolUse = &ToolUse{
				Name: contentBlock.Name,
				ID:   contentBlock.ID,
			}
			fmt.Printf("### Tool: `%s`\n\n", contentBlock.Name)
			fmt.Printf("```json\n")
		}

	case "content_block_delta":
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		json.Unmarshal(event.Delta, &delta)

		if delta.Type == "text_delta" {
			// Accumulate text
			currentTextBlock.WriteString(delta.Text)
		} else if delta.Type == "input_json_delta" {
			// Accumulate tool input JSON
			if *currentToolUse != nil {
				(*currentToolUse).Input.WriteString(delta.PartialJSON)
			}
		}

	case "content_block_stop":
		if *currentToolUse != nil {
			// End of tool use block
			fmt.Println((*currentToolUse).Input.String())
			fmt.Printf("```\n\n")
			*currentToolUse = nil
		} else if currentTextBlock.Len() > 0 {
			// End of text block - but don't flush yet, might be more text coming
		}

	case "message_stop":
		// End of entire message
		if currentTextBlock.Len() > 0 {
			fmt.Print(currentTextBlock.String())
			fmt.Println("\n")
			currentTextBlock.Reset()
		}
		fmt.Println("---\n")
	}
}
