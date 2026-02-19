package main

import (
	"context"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewStrandTool(t *testing.T) {
	tool := NewStrandTool()
	if tool == nil {
		t.Fatal("expected StrandTool to be created")
	}
}

func TestStrandToolWithProjectDir(t *testing.T) {
	os.Setenv("STRAND_PROJECT_DIR", "/test/project")
	defer os.Unsetenv("STRAND_PROJECT_DIR")

	tool := NewStrandTool()
	if tool.projectDir != "/test/project" {
		t.Errorf("expected projectDir '/test/project', got %s", tool.projectDir)
	}
}

func TestListTasksArgs(t *testing.T) {
	args := ListTasksArgs{
		Role:   "developer",
		Status: "open",
	}

	if args.Role != "developer" {
		t.Errorf("expected role 'developer', got %s", args.Role)
	}
	if args.Status != "open" {
		t.Errorf("expected status 'open', got %s", args.Status)
	}
}

func TestGetTaskArgs(t *testing.T) {
	args := GetTaskArgs{
		TaskID: "T1a2b3c",
	}

	if args.TaskID != "T1a2b3c" {
		t.Errorf("expected taskID 'T1a2b3c', got %s", args.TaskID)
	}
}

func TestCompleteTaskArgs(t *testing.T) {
	todoNum := 1
	args := CompleteTaskArgs{
		TaskID: "T1a2b3c",
		Todo:   &todoNum,
		Role:   "developer",
		Report: "Task completed successfully",
	}

	if args.TaskID != "T1a2b3c" {
		t.Errorf("expected taskID 'T1a2b3c', got %s", args.TaskID)
	}
	if args.Todo == nil || *args.Todo != 1 {
		t.Error("expected todo to be 1")
	}
	if args.Role != "developer" {
		t.Errorf("expected role 'developer', got %s", args.Role)
	}
	if args.Report != "Task completed successfully" {
		t.Errorf("expected report, got %s", args.Report)
	}
}

func TestAddTaskArgs(t *testing.T) {
	args := AddTaskArgs{
		Type:     "task",
		Title:    "Test Task",
		Body:     "This is a test task",
		Role:     "developer",
		Priority: "high",
		Parent:   "T1a2b",
	}

	if args.Type != "task" {
		t.Errorf("expected type 'task', got %s", args.Type)
	}
	if args.Title != "Test Task" {
		t.Errorf("expected title 'Test Task', got %s", args.Title)
	}
	if args.Priority != "high" {
		t.Errorf("expected priority 'high', got %s", args.Priority)
	}
}

func TestExtractTaskID(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "with created task prefix",
			output:   "Created task: T1a2b3c\nTask details...",
			expected: "T1a2b3c",
		},
		{
			name:     "with task ID prefix",
			output:   "Task ID: Taj3cp2\nDetails...",
			expected: "Taj3cp2",
		},
		{
			name:     "standalone task ID",
			output:   "T1a2b3c",
			expected: "T1a2b3c",
		},
		{
			name:     "no task ID",
			output:   "No task created",
			expected: "",
		},
		{
			name:     "task ID in middle",
			output:   "The task Taj3cp2 was created successfully",
			expected: "Taj3cp2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTaskID(tt.output)
			if result != tt.expected {
				t.Errorf("extractTaskID() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestListTasksValidation(t *testing.T) {
	tool := NewStrandTool()
	ctx := context.Background()

	// This test verifies the function signature is correct
	// We can't test actual execution without a real strand CLI
	_, _, err := tool.listTasks(ctx, &mcp.CallToolRequest{}, ListTasksArgs{})
	// Error is expected since strand may not be available or configured
	// The important thing is the function signature works
	_ = err
}

func TestGetTaskValidation(t *testing.T) {
	tool := NewStrandTool()
	ctx := context.Background()

	// Test missing task ID
	result, _, err := tool.getTask(ctx, &mcp.CallToolRequest{}, GetTaskArgs{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.IsError {
		t.Error("expected error result for missing task_id")
	}
}

func TestCompleteTaskValidation(t *testing.T) {
	tool := NewStrandTool()
	ctx := context.Background()

	// Test missing task ID
	result, _, err := tool.completeTask(ctx, &mcp.CallToolRequest{}, CompleteTaskArgs{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.IsError {
		t.Error("expected error result for missing task_id")
	}
}

func TestAddTaskValidation(t *testing.T) {
	tool := NewStrandTool()
	ctx := context.Background()

	// Test missing type
	result, _, err := tool.addTask(ctx, &mcp.CallToolRequest{}, AddTaskArgs{Title: "Test"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.IsError {
		t.Error("expected error result for missing type")
	}

	// Test missing title
	result, _, err = tool.addTask(ctx, &mcp.CallToolRequest{}, AddTaskArgs{Type: "task"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.IsError {
		t.Error("expected error result for missing title")
	}
}

func TestIsValidTaskID(t *testing.T) {
	valid := []struct {
		input string
		desc  string
	}{
		{"T1a2b3c", "typical short ID"},
		{"Taj3cp2", "mixed alpha+digit"},
		{"T1a2b3c4d5", "longer ID"},
		{"T1a2b3c,", "trailing comma stripped"},
		{"T1a2b3c.", "trailing period stripped"},
		{"T1a2b3c:", "trailing colon stripped"},
		{"T1a2b3c;", "trailing semicolon stripped"},
		{"T1ABCDE", "uppercase after T"},
		{"T12", "minimal length (3 chars)"},
	}
	for _, tt := range valid {
		if !isValidTaskID(tt.input) {
			t.Errorf("expected %q (%s) to be valid", tt.input, tt.desc)
		}
	}

	invalid := []struct {
		input string
		desc  string
	}{
		{"", "empty string"},
		{"T", "too short (1 char)"},
		{"Ta", "too short (2 chars), no digit"},
		{"Task", "word starting with T, no digit"},
		{"The", "word starting with T, no digit"},
		{"1abc", "doesn't start with T"},
		{"abc123", "no T prefix"},
		{"T1-2", "hyphen not allowed in middle"},
		{"T1 2", "space not allowed"},
		{"T1a2b3c4d5e6f7g8h9i0j", "too long (>20 chars)"},
		{"Tabcdefg", "all alpha after T, no digit"},
	}
	for _, tt := range invalid {
		if isValidTaskID(tt.input) {
			t.Errorf("expected %q (%s) to be invalid", tt.input, tt.desc)
		}
	}
}

func TestClaimTaskValidation(t *testing.T) {
	tool := NewStrandTool()
	ctx := context.Background()

	// Test missing task ID
	result, _, err := tool.claimTask(ctx, &mcp.CallToolRequest{}, ClaimTaskArgs{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.IsError {
		t.Error("expected error result for missing task_id")
	}
}
