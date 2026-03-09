package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/ricochet1k/monty-wasm-go/pkg/monty"

	"github.com/ricochet1k/orbitmesh/internal/toolcall"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

var runCodeExternalFunctions = []string{
	"list_tasks", "get_task", "next_task", "complete_task", "add_task", "claim_task", "webfetch",
}

func RegisterDefaultTools(reg Registry) error {
	defs := []ToolDef{
		{Name: "read_file", Description: "Read the contents of a file at the given path", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative file path"}},"required":["path"]}`), Handler: readFileTool},
		{Name: "bash", Description: "Execute a bash command and return its output", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The bash command to execute"}},"required":["command"]}`), Handler: bashTool},
		{Name: "edit", Description: "Make exact string replacements in a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"oldString":{"type":"string"},"newString":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["path","oldString","newString"]}`), Handler: editTool},
		{Name: "write_file", Description: "Write content to a file at the given path", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`), Handler: writeFileTool},
		{Name: "list_tasks", Description: "List tasks with optional filtering by role or status", InputSchema: json.RawMessage(`{"type":"object","properties":{"role":{"type":"string"},"status":{"type":"string"}}}`), Handler: listTasksTool},
		{Name: "get_task", Description: "Get full details of a specific task by ID", InputSchema: json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`), Handler: getTaskTool},
		{Name: "next_task", Description: "Get the next available task for a role, optionally claiming it", InputSchema: json.RawMessage(`{"type":"object","properties":{"role":{"type":"string"},"claim":{"type":"boolean"}}}`), Handler: nextTaskTool},
		{Name: "complete_task", Description: "Mark a task or specific todo as completed", InputSchema: json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"todo":{"type":"integer"},"role":{"type":"string"},"report":{"type":"string"}},"required":["task_id"]}`), Handler: completeTaskTool},
		{Name: "add_task", Description: "Create a new task using a template", InputSchema: json.RawMessage(`{"type":"object","properties":{"type":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"role":{"type":"string"},"priority":{"type":"string"},"parent":{"type":"string"}},"required":["type","title"]}`), Handler: addTaskTool},
		{Name: "claim_task", Description: "Claim a task by marking it in progress", InputSchema: json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`), Handler: claimTaskTool},
		{Name: "webfetch", Description: "Fetch a URL, convert HTML to Markdown, and optionally filter by regex with context", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"regex":{"type":"string"},"context_lines":{"type":"integer"},"timeout_seconds":{"type":"integer"}},"required":["url"]}`), Handler: webfetchTool},
		{Name: "run_code", Description: "Run code in a restricted subset of Python with top-level async support", InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}`), AsyncHandler: &AsyncHandler{Run: runCodeToolRun, ResumeFunc: runCodeToolResume}},
		{Name: "list_ui_components", Description: "List MCP-enabled UI components available on the live page", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`), Handler: listUIComponentsTool},
		{Name: "dispatch_ui_action", Description: "Dispatch an MCP UI action to a component on the live page", InputSchema: json.RawMessage(`{"type":"object","properties":{"component_id":{"type":"string"},"action":{"type":"string"},"payload":{}},"required":["component_id","action"]}`), Handler: dispatchUIActionTool},
		{Name: "multi_edit_ui", Description: "Edit multiple MCP input components on the live page", InputSchema: json.RawMessage(`{"type":"object","properties":{"fields":{}},"required":["fields"]}`), Handler: multiEditUITool},
	}
	for _, def := range defs {
		if err := reg.Register(def); err != nil && !strings.Contains(err.Error(), "already registered") {
			return err
		}
	}
	return nil
}

func readFileTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	b, err := os.ReadFile(a.Path)
	return string(b), err
}
func bashTool(ctx context.Context, input json.RawMessage) (string, error) {
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "bash", "-c", a.Command).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return string(out), nil
}
func editTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		Path, OldString, NewString string
		ReplaceAll                 bool `json:"replaceAll"`
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		return "", err
	}
	s := string(b)
	ns := s
	if a.ReplaceAll {
		ns = strings.ReplaceAll(s, a.OldString, a.NewString)
	} else {
		i := strings.Index(s, a.OldString)
		if i < 0 {
			return "", errors.New("no matches found for oldString")
		}
		ns = s[:i] + a.NewString + s[i+len(a.OldString):]
	}
	if ns == s {
		return "", errors.New("no matches found for oldString")
	}
	if err := os.WriteFile(a.Path, []byte(ns), 0o644); err != nil {
		return "", err
	}
	return "edit completed successfully", nil
}
func writeFileTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct{ Path, Content string }
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
}

func execStrand(args []string, stdin string) (string, error) {
	projectDir := strings.TrimSpace(os.Getenv("STRAND_PROJECT_DIR"))
	if projectDir != "" {
		args = append([]string{"--project", projectDir}, args...)
	}
	cmd := exec.Command("strand", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("strand command failed: %w: %s", err, out)
	}
	return string(out), nil
}

func listTasksTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct{ Role, Status string }
	_ = json.Unmarshal(input, &a)
	args := []string{"list", "--format", "json"}
	if a.Role != "" {
		args = append(args, "--role", a.Role)
	}
	if a.Status != "" {
		args = append(args, "--status", a.Status)
	}
	return execStrand(args, "")
}
func getTaskTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	if a.TaskID == "" {
		return "", errors.New("task_id is required")
	}
	return execStrand([]string{"show", a.TaskID}, "")
}
func nextTaskTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		Role  string
		Claim bool
	}
	_ = json.Unmarshal(input, &a)
	args := []string{"next"}
	if a.Role != "" {
		args = append(args, "--role", a.Role)
	}
	if a.Claim {
		args = append(args, "--claim")
	}
	return execStrand(args, "")
}
func completeTaskTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		TaskID       string `json:"task_id"`
		Todo         *int   `json:"todo"`
		Role, Report string
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	if a.TaskID == "" {
		return "", errors.New("task_id is required")
	}
	args := []string{"complete", a.TaskID}
	if a.Todo != nil {
		args = append(args, "--todo", strconv.Itoa(*a.Todo))
	}
	if a.Role != "" {
		args = append(args, "--role", a.Role)
	}
	return execStrand(args, a.Report)
}
func addTaskTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct{ Type, Title, Body, Role, Priority, Parent string }
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	if a.Type == "" || a.Title == "" {
		return "", errors.New("type and title are required")
	}
	args := []string{"add", a.Type, a.Title}
	if a.Role != "" {
		args = append(args, "--role", a.Role)
	}
	if a.Priority != "" {
		args = append(args, "--priority", a.Priority)
	}
	if a.Parent != "" {
		args = append(args, "--parent", a.Parent)
	}
	return execStrand(args, a.Body)
}
func claimTaskTool(_ context.Context, input json.RawMessage) (string, error) {
	var a struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(input, &a); err != nil {
		return "", err
	}
	if a.TaskID == "" {
		return "", errors.New("task_id is required")
	}
	return execStrand([]string{"claim", a.TaskID}, "")
}

func webfetchTool(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		URL            string `json:"url"`
		Regex          string `json:"regex"`
		ContextLines   int    `json:"context_lines"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return "", errors.New("url is required")
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("url must be a valid http/https URL")
	}
	timeout := 20 * time.Second
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	converter := htmltomarkdown.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(string(body))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Regex) == "" {
		return markdown, nil
	}
	return filterMarkdownByRegex(markdown, args.Regex, args.ContextLines)
}

func filterMarkdownByRegex(markdown string, pattern string, contextLines int) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	if contextLines <= 0 {
		contextLines = 2
	}
	lines := strings.Split(markdown, "\n")
	matchIdx := make([]int, 0)
	for i, line := range lines {
		if re.MatchString(line) {
			matchIdx = append(matchIdx, i)
		}
	}
	if len(matchIdx) == 0 {
		return "No lines matched regex: " + pattern, nil
	}
	type section struct{ start, end int }
	ranges := make([]section, 0, len(matchIdx))
	for _, idx := range matchIdx {
		start := idx - contextLines
		if start < 0 {
			start = 0
		}
		end := idx + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}
		ranges = append(ranges, section{start: start, end: end})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	merged := make([]section, 0, len(ranges))
	for _, r := range ranges {
		if len(merged) == 0 || r.start > merged[len(merged)-1].end+1 {
			merged = append(merged, r)
			continue
		}
		if r.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = r.end
		}
	}
	var b strings.Builder
	for i, r := range merged {
		if i > 0 {
			b.WriteString("\n...\n")
		}
		for ln := r.start; ln <= r.end; ln++ {
			b.WriteString(strconv.Itoa(ln + 1))
			b.WriteString(": ")
			b.WriteString(lines[ln])
			if ln < r.end {
				b.WriteString("\n")
			}
		}
	}
	return b.String(), nil
}

type runCodeHandlerState struct {
	Dependency  toolcall.Dependency `json:"dependency"`
	SnapshotB64 string              `json:"snapshot_b64"`
}

func runCodeToolRun(ctx context.Context, input json.RawMessage, h EvalHandle) error {
	var args struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return err
	}
	code := strings.TrimSpace(args.Code)
	if code == "" {
		return errors.New("code is required")
	}
	runtime, err := monty.NewRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close(ctx)
	program, err := runtime.Compile(ctx, code, monty.CompileOptions{ScriptName: "run_code.py", ExternalFunctions: runCodeExternalFunctions})
	if err != nil {
		return err
	}
	defer program.Close(ctx)
	progress, err := program.Start(ctx, []any{}...)
	if err != nil {
		return err
	}
	return runCodeAdvance(ctx, h, progress)
}

func runCodeToolResume(ctx context.Context, h EvalHandle) error {
	var state runCodeHandlerState
	if err := json.Unmarshal(h.State(), &state); err != nil {
		return err
	}
	if state.Dependency.Kind == "" || state.Dependency.ID == "" {
		return errors.New("run_code resume missing dependency state")
	}
	if strings.TrimSpace(state.SnapshotB64) == "" {
		return errors.New("run_code resume missing continuation snapshot")
	}
	snapshot, err := base64.StdEncoding.DecodeString(state.SnapshotB64)
	if err != nil {
		return fmt.Errorf("decode run_code continuation snapshot: %w", err)
	}
	runtime, err := monty.NewRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close(ctx)
	continuation, err := runtime.LoadContinuation(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("load run_code continuation snapshot: %w", err)
	}
	defer continuation.Close(ctx)
	result, isError, err := h.ResolveDependency(state.Dependency)
	if err != nil {
		return err
	}
	var progress monty.Progress
	if isError {
		progress, err = continuation.Throw(ctx, result)
	} else {
		returned := runCodeParseResult(result)
		progress, err = continuation.Return(ctx, returned)
	}
	if err != nil {
		return err
	}
	return runCodeAdvance(ctx, h, progress)
}

func runCodeAdvance(ctx context.Context, h EvalHandle, progress monty.Progress) error {
	for {
		switch progress.Kind {
		case monty.KindComplete:
			h.Complete(string(progress.Result))
			return nil
		case monty.KindFunctionCall:
			if progress.Call == nil {
				return errors.New("runtime produced empty function call")
			}
			if !containsString(runCodeExternalFunctions, progress.Call.Name) {
				progressNext, err := progress.Call.Throw(ctx, fmt.Sprintf("unknown external function: %s", progress.Call.Name))
				if err != nil {
					return err
				}
				progress = progressNext
				continue
			}
			rawInput, err := runCodeCallInput(progress.Call)
			if err != nil {
				return err
			}
			dep, err := h.DispatchSubtool(progress.Call.Name, rawInput)
			if err != nil {
				return err
			}
			snapshot, err := progress.Call.Dump(ctx)
			if err != nil {
				return err
			}
			progress.Call.Close(ctx)
			stateRaw, err := json.Marshal(runCodeHandlerState{Dependency: dep, SnapshotB64: base64.StdEncoding.EncodeToString(snapshot)})
			if err != nil {
				return err
			}
			if err := h.Suspend(stateRaw, []toolcall.Dependency{dep}); err != nil {
				return err
			}
			return nil
		case monty.KindOSCall:
			if progress.OSCall == nil {
				return errors.New("runtime produced empty OS call")
			}
			nextProgress, err := progress.OSCall.Throw(ctx, fmt.Sprintf("OS call %q is not permitted", progress.OSCall.Name))
			if err != nil {
				return err
			}
			progress = nextProgress
		default:
			return fmt.Errorf("unsupported runtime state: %v", progress.Kind)
		}
	}
}

func runCodeCallInput(call *monty.Call) (json.RawMessage, error) {
	args := make(map[string]any, len(call.Kwargs))
	for i, kw := range call.Kwargs {
		var k string
		if err := kw.Key.Decode(&k); err != nil {
			return nil, fmt.Errorf("decode kwarg key %d: %w", i, err)
		}
		var v any
		if err := kw.Value.Decode(&v); err != nil {
			return nil, fmt.Errorf("decode kwarg value %q: %w", k, err)
		}
		args[k] = v
	}
	if len(args) == 0 && len(call.Args) > 0 {
		args["arg0"] = call.Args
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func runCodeParseResult(result string) any {
	var parsed any
	if json.Unmarshal([]byte(result), &parsed) == nil {
		return parsed
	}
	return result
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func listUIComponentsTool(ctx context.Context, _ json.RawMessage) (string, error) {
	return frontendToolsRequest(ctx, apiTypes.DockMCPRequest{Kind: "list"})
}

func dispatchUIActionTool(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		ComponentID string `json:"component_id"`
		Action      string `json:"action"`
		Payload     any    `json:"payload"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.ComponentID) == "" || strings.TrimSpace(args.Action) == "" {
		return "", errors.New("component_id and action are required")
	}
	return frontendToolsRequest(ctx, apiTypes.DockMCPRequest{Kind: "dispatch", TargetID: args.ComponentID, Action: args.Action, Payload: args.Payload})
}

func multiEditUITool(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Fields any `json:"fields"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	return frontendToolsRequest(ctx, apiTypes.DockMCPRequest{Kind: "multi_edit", Payload: args.Fields})
}

func frontendToolsRequest(ctx context.Context, request apiTypes.DockMCPRequest) (string, error) {
	sessionID := strings.TrimSpace(SessionIDFromContext(ctx))
	if sessionID == "" {
		return "", errors.New("missing session context for frontend tools tool")
	}
	baseURL := strings.TrimSpace(APIBaseURLFromContext(ctx))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/sessions/%s/frontend-tools/mcp/request", baseURL, sessionID)
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Orbitmesh-Internal", "frontend-tools-mcp")
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("dock request failed: %s", strings.TrimSpace(string(respBody)))
	}
	var out apiTypes.DockMCPResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	jsonBody, _ := json.Marshal(out.Result)
	return string(jsonBody), nil
}
