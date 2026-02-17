package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/provider/common/claude"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: test_parser <file.ndjson>")
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

	sessionID := "test-session-1"
	lineNum := 0

	fmt.Println("=== Testing OrbitMesh Claude Provider Parser ===\n")

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		// Parse the message using OrbitMesh parser
		msg, err := claude.ParseMessage(line)
		if err != nil {
			fmt.Printf("❌ Line %d: Parse error: %v\n", lineNum, err)
			continue
		}

		// Translate to OrbitMesh event
		event, shouldEmit := claude.TranslateToOrbitMeshEvent(sessionID, msg)

		if shouldEmit {
			printEvent(lineNum, msg.Type, event)
		} else {
			// Show non-emitted messages for debugging
			fmt.Printf("   Line %d: %s (no event)\n", lineNum, msg.Type)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Processed %d lines\n", lineNum)
}

func printEvent(lineNum int, msgType claude.MessageType, event domain.Event) {
	// fmt.Printf("📨 Line %d: %s -> %s\n", lineNum, msgType, event.Type)

	switch event.Type {
	case domain.EventTypeOutput:
		if data, ok := event.Output(); ok {
			// Truncate long output
			content := data.Content
			// if len(content) > 100 {
			// 	content = content[:100] + "..."
			// }
			// content = strings.ReplaceAll(content, "\n", "\\n")
			// fmt.Printf("   📝 Output: %s\n", content)
			fmt.Print(content)
		}

	case domain.EventTypeMetadata:
		if data, ok := event.Metadata(); ok {
			fmt.Printf("   ℹ️  %s: %+v\n", data.Key, data.Value)
		}

	case domain.EventTypeMetric:
		if data, ok := event.Metric(); ok {
			fmt.Printf("   📊 Tokens: in=%d out=%d requests=%d\n",
				data.TokensIn, data.TokensOut, data.RequestCount)
		}

	case domain.EventTypeError:
		if data, ok := event.Error(); ok {
			fmt.Printf("   ❌ Error: %s (%s)\n", data.Message, data.Code)
		}

	case domain.EventTypeStatusChange:
		if data, ok := event.StatusChange(); ok {
			fmt.Printf("   🔄 Status: %s -> %s (%s)\n",
				data.OldState, data.NewState, data.Reason)
		}
	}

	fmt.Println()
}
