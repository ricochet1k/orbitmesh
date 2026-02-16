package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

const (
	serverName    = "orbitmesh-mcp"
	serverVersion = "1.0.0"
)

type StrandTool struct {
	projectDir string
}

func NewStrandTool() *StrandTool {
	return &StrandTool{
		projectDir: os.Getenv("STRAND_PROJECT_DIR"),
	}
}

func (s *StrandTool) execStrand(args ...string) (string, error) {
	if s.projectDir != "" {
		args = append([]string{"--project", s.projectDir}, args...)
	}
	cmd := exec.Command("strand", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("strand command failed: %w: %s", err, string(output))
	}
	return string(output), nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mode := os.Getenv("ORBITMESH_MCP_MODE")
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "" {
		mode = "strand"
	}

	impl := &mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}

	server := mcp.NewServer(impl, nil)

	switch mode {
	case "dock":
		dockTool := NewDockTool()
		registerDockTools(server, dockTool)
	default:
		tool := NewStrandTool()
		registerStrandTools(server, tool)
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func registerStrandTools(server *mcp.Server, tool *StrandTool) {
	// Register list_tasks tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List tasks with optional filtering by role or status",
	}, tool.listTasks)

	// Register get_task tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Description: "Get full details of a specific task by ID",
	}, tool.getTask)

	// Register next_task tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "next_task",
		Description: "Get the next available task for a role, optionally claiming it",
	}, tool.nextTask)

	// Register complete_task tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task or specific todo as completed",
	}, tool.completeTask)

	// Register add_task tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_task",
		Description: "Create a new task using a template",
	}, tool.addTask)

	// Register claim_task tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_task",
		Description: "Claim a task by marking it in progress",
	}, tool.claimTask)
}

func registerDockTools(server *mcp.Server, tool *DockTool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_ui_components",
		Description: "List MCP-enabled UI components available on the live page",
	}, tool.listComponents)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dispatch_ui_action",
		Description: "Dispatch an MCP UI action to a component on the live page",
	}, tool.dispatchAction)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "multi_edit_ui",
		Description: "Edit multiple MCP input components on the live page",
	}, tool.multiEdit)
}

type ListTasksArgs struct {
	Role   string `json:"role,omitempty" jsonschema:"description=Filter tasks by role (e.g. developer reviewer tester)"`
	Status string `json:"status,omitempty" jsonschema:"description=Filter tasks by status (e.g. open in_progress completed)"`
}

type GetTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"description=The task ID (e.g. T1a2b3c),required"`
}

type NextTaskArgs struct {
	Role  string `json:"role,omitempty" jsonschema:"description=Filter by role (e.g. developer reviewer)"`
	Claim bool   `json:"claim,omitempty" jsonschema:"description=Claim the task by marking it in_progress,default=false"`
}

type CompleteTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"description=The task ID (e.g. T1a2b3c),required"`
	Todo   *int   `json:"todo,omitempty" jsonschema:"description=Optional: 1-based index of specific todo to complete"`
	Role   string `json:"role,omitempty" jsonschema:"description=Optional: validate role matches task role"`
	Report string `json:"report,omitempty" jsonschema:"description=Optional: completion report or notes"`
}

type AddTaskArgs struct {
	Type     string `json:"type" jsonschema:"description=Task template type (e.g. task issue review),required"`
	Title    string `json:"title" jsonschema:"description=Task title,required"`
	Body     string `json:"body,omitempty" jsonschema:"description=Task description/body content"`
	Role     string `json:"role,omitempty" jsonschema:"description=Role responsible for the task"`
	Priority string `json:"priority,omitempty" jsonschema:"description=Priority: high medium or low,enum=high,enum=medium,enum=low"`
	Parent   string `json:"parent,omitempty" jsonschema:"description=Parent task ID for creating subtasks"`
}

type ClaimTaskArgs struct {
	TaskID string `json:"task_id" jsonschema:"description=The task ID to claim,required"`
}

func (s *StrandTool) listTasks(ctx context.Context, req *mcp.CallToolRequest, args ListTasksArgs) (*mcp.CallToolResult, any, error) {
	cmdArgs := []string{"list", "--format", "json"}

	if args.Role != "" {
		cmdArgs = append(cmdArgs, "--role", args.Role)
	}
	if args.Status != "" {
		cmdArgs = append(cmdArgs, "--status", args.Status)
	}

	output, err := s.execStrand(cmdArgs...)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error listing tasks: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: output},
		},
	}, nil, nil
}

func (s *StrandTool) getTask(ctx context.Context, req *mcp.CallToolRequest, args GetTaskArgs) (*mcp.CallToolResult, any, error) {
	if args.TaskID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "task_id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	output, err := s.execStrand("show", args.TaskID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error getting task %s: %v", args.TaskID, err)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: output},
		},
	}, nil, nil
}

func (s *StrandTool) nextTask(ctx context.Context, req *mcp.CallToolRequest, args NextTaskArgs) (*mcp.CallToolResult, any, error) {
	cmdArgs := []string{"next"}

	if args.Role != "" {
		cmdArgs = append(cmdArgs, "--role", args.Role)
	}

	if args.Claim {
		cmdArgs = append(cmdArgs, "--claim")
	}

	output, err := s.execStrand(cmdArgs...)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error getting next task: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: output},
		},
	}, nil, nil
}

func (s *StrandTool) completeTask(ctx context.Context, req *mcp.CallToolRequest, args CompleteTaskArgs) (*mcp.CallToolResult, any, error) {
	if args.TaskID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "task_id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	cmdArgs := []string{"complete", args.TaskID}

	if args.Todo != nil {
		cmdArgs = append(cmdArgs, "--todo", fmt.Sprintf("%d", *args.Todo))
	}

	if args.Role != "" {
		cmdArgs = append(cmdArgs, "--role", args.Role)
	}

	var output string
	var err error

	if args.Report != "" {
		cmd := exec.Command("strand", cmdArgs...)
		if s.projectDir != "" {
			cmdArgs = append([]string{"--project", s.projectDir}, cmdArgs...)
			cmd = exec.Command("strand", cmdArgs...)
		}
		cmd.Stdin = strings.NewReader(args.Report)
		out, execErr := cmd.CombinedOutput()
		output = string(out)
		err = execErr
	} else {
		output, err = s.execStrand(cmdArgs...)
	}

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error completing task %s: %v\nOutput: %s", args.TaskID, err, output)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Task %s completed successfully.\n%s", args.TaskID, output)},
		},
	}, nil, nil
}

func (s *StrandTool) addTask(ctx context.Context, req *mcp.CallToolRequest, args AddTaskArgs) (*mcp.CallToolResult, any, error) {
	if args.Type == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "type is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.Title == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "title is required"},
			},
			IsError: true,
		}, nil, nil
	}

	cmdArgs := []string{"add", args.Type, args.Title}

	if args.Role != "" {
		cmdArgs = append(cmdArgs, "--role", args.Role)
	}

	if args.Priority != "" {
		cmdArgs = append(cmdArgs, "--priority", args.Priority)
	}

	if args.Parent != "" {
		cmdArgs = append(cmdArgs, "--parent", args.Parent)
	}

	var output string
	var err error

	if args.Body != "" {
		cmd := exec.Command("strand", cmdArgs...)
		if s.projectDir != "" {
			cmdArgs = append([]string{"--project", s.projectDir}, cmdArgs...)
			cmd = exec.Command("strand", cmdArgs...)
		}
		cmd.Stdin = strings.NewReader(args.Body)
		out, execErr := cmd.CombinedOutput()
		output = string(out)
		err = execErr
	} else {
		output, err = s.execStrand(cmdArgs...)
	}

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error creating task: %v\nOutput: %s", err, output)},
			},
			IsError: true,
		}, nil, nil
	}

	// Try to extract task ID from output
	taskID := extractTaskID(output)
	resultMsg := fmt.Sprintf("Task created successfully.\n%s", output)
	if taskID != "" {
		resultMsg = fmt.Sprintf("Task %s created successfully.\n%s", taskID, output)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultMsg},
		},
	}, nil, nil
}

func (s *StrandTool) claimTask(ctx context.Context, req *mcp.CallToolRequest, args ClaimTaskArgs) (*mcp.CallToolResult, any, error) {
	if args.TaskID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "task_id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	output, err := s.execStrand("claim", args.TaskID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error claiming task %s: %v\nOutput: %s", args.TaskID, err, output)},
			},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Task %s claimed successfully.\n%s", args.TaskID, output)},
		},
	}, nil, nil
}

func extractTaskID(output string) string {
	// Look for pattern like "T1a2b3c" or "Created task: T1a2b3c"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Created task:") || strings.Contains(line, "Task ID:") {
			fields := strings.Fields(line)
			for _, field := range fields {
				if isValidTaskID(field) {
					return field
				}
			}
		}
	}

	// Try to find any token starting with T followed by alphanumeric
	for _, line := range lines {
		fields := strings.Fields(line)
		for _, field := range fields {
			if isValidTaskID(field) {
				return field
			}
		}
	}

	return ""
}

func isValidTaskID(s string) bool {
	// Task IDs start with T and are followed by alphanumeric characters
	// Typical pattern: T1a2b3c, Taj3cp2, etc.
	// Must contain at least one digit to distinguish from words like "Task", "The"
	if !strings.HasPrefix(s, "T") {
		return false
	}
	if len(s) < 3 || len(s) > 20 {
		return false
	}

	hasDigit := false
	// Check that the rest is alphanumeric
	for i, c := range s[1:] {
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			// Allow trailing punctuation but strip it
			if i > 0 && (c == ',' || c == '.' || c == ':' || c == ';') {
				return hasDigit
			}
			return false
		}
	}
	return hasDigit
}

type DockTool struct {
	baseURL   string
	sessionID string
	client    *http.Client
}

func NewDockTool() *DockTool {
	baseURL := os.Getenv("ORBITMESH_API_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	return &DockTool{
		baseURL:   strings.TrimRight(baseURL, "/"),
		sessionID: os.Getenv("ORBITMESH_DOCK_SESSION_ID"),
		client:    &http.Client{Timeout: 45 * time.Second},
	}
}

type DispatchUIArgs struct {
	ComponentID string `json:"component_id" jsonschema:"description=Target MCP component ID,required"`
	Action      string `json:"action" jsonschema:"description=Action type (click edit read focus select toggle),required"`
	Payload     any    `json:"payload,omitempty" jsonschema:"description=Optional action payload"`
}

type MultiEditArgs struct {
	Fields any `json:"fields" jsonschema:"description=Field map or list for multi-edit,required"`
}

func (d *DockTool) listComponents(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	resp, err := d.request(ctx, apiTypes.DockMCPRequest{Kind: "list"})
	return dockResult(resp, err)
}

func (d *DockTool) dispatchAction(ctx context.Context, req *mcp.CallToolRequest, args DispatchUIArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.ComponentID) == "" || strings.TrimSpace(args.Action) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "component_id and action are required"},
			},
			IsError: true,
		}, nil, nil
	}
	resp, err := d.request(ctx, apiTypes.DockMCPRequest{
		Kind:     "dispatch",
		TargetID: args.ComponentID,
		Action:   args.Action,
		Payload:  args.Payload,
	})
	return dockResult(resp, err)
}

func (d *DockTool) multiEdit(ctx context.Context, req *mcp.CallToolRequest, args MultiEditArgs) (*mcp.CallToolResult, any, error) {
	resp, err := d.request(ctx, apiTypes.DockMCPRequest{
		Kind:    "multi_edit",
		Payload: args.Fields,
	})
	return dockResult(resp, err)
}

func (d *DockTool) request(ctx context.Context, request apiTypes.DockMCPRequest) (apiTypes.DockMCPResponse, error) {
	if d.sessionID == "" {
		return apiTypes.DockMCPResponse{}, fmt.Errorf("missing ORBITMESH_DOCK_SESSION_ID")
	}
	url := fmt.Sprintf("%s/api/sessions/%s/dock/mcp/request", d.baseURL, d.sessionID)

	body, err := json.Marshal(request)
	if err != nil {
		return apiTypes.DockMCPResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return apiTypes.DockMCPResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Orbitmesh-Internal", "dock-mcp")

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return apiTypes.DockMCPResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiTypes.DockMCPResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiTypes.DockMCPResponse{}, fmt.Errorf("dock request failed: %s", strings.TrimSpace(string(respBody)))
	}

	var response apiTypes.DockMCPResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return apiTypes.DockMCPResponse{}, err
	}
	return response, nil
}

func dockResult(resp apiTypes.DockMCPResponse, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Dock request failed: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}
	if resp.Error != "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: resp.Error},
			},
			IsError: true,
		}, nil, nil
	}
	jsonBody, _ := json.Marshal(resp.Result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonBody)},
		},
	}, nil, nil
}
