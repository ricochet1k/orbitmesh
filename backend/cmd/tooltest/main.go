// tooltest is a helper CLI for testing tool calls end-to-end against an
// orbitmesh instance.
//
// Usage:
//
//	tooltest [flags]
//	  -session  string   Use existing session ID (creates new if not set)
//	  -provider string   Provider ID to use (default: prov_8ee0342abddb8e21)
//	  -message  string   Message to send
//	  -url      string   Base URL (default: http://localhost:8080)
//	  -watch             Just watch events on an existing session (don't send message)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// --------------------------------------------------------------------------
// Minimal API types (mirrors pkg/api/types.go — no import of the package
// itself so this binary stays dependency-free beyond the stdlib).
// --------------------------------------------------------------------------

type sessionRequest struct {
	ProviderType string `json:"provider_type,omitempty"`
	ProviderID   string `json:"provider_id,omitempty"`
	Title        string `json:"title,omitempty"`
}

type sessionResponse struct {
	ID                  string    `json:"id"`
	ProviderType        string    `json:"provider_type"`
	PreferredProviderID string    `json:"preferred_provider_id,omitempty"`
	Title               string    `json:"title,omitempty"`
	State               string    `json:"state"`
	WorkingDir          string    `json:"working_dir"`
	CreatedAt           time.Time `json:"created_at"`
}

type sendMessageRequest struct {
	Content    string `json:"content"`
	ProviderID string `json:"provider_id,omitempty"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// sseEvent is the parsed representation of one SSE frame.
type sseEvent struct {
	id    string
	event string
	data  string
}

// --------------------------------------------------------------------------
// ANSI color helpers
// --------------------------------------------------------------------------

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

func colored(c, s string) string {
	return c + s + colorReset
}

// --------------------------------------------------------------------------
// HTTP client with CSRF support
// --------------------------------------------------------------------------

// client holds an http.Client plus a CSRF token obtained from the server.
type client struct {
	hc        *http.Client
	csrfToken string
	cookies   []*http.Cookie
}

func newClient(baseURL string) (*client, error) {
	hc := &http.Client{Timeout: 10 * time.Second}
	// Do a GET to obtain the CSRF cookie.
	resp, err := hc.Get(baseURL + "/api/sessions")
	if err != nil {
		return nil, fmt.Errorf("CSRF bootstrap GET: %w", err)
	}
	defer resp.Body.Close()
	// Discard body
	_, _ = io.Copy(io.Discard, resp.Body)

	var csrfToken string
	var cookies []*http.Cookie
	for _, c := range resp.Cookies() {
		cookies = append(cookies, c)
		if c.Name == "orbitmesh-csrf-token" {
			csrfToken = c.Value
		}
	}
	if csrfToken == "" {
		return nil, fmt.Errorf("CSRF bootstrap: no csrf cookie in response")
	}
	return &client{hc: hc, csrfToken: csrfToken, cookies: cookies}, nil
}

func (c *client) postJSON(url string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrfToken)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	return c.hc.Do(req)
}

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// --------------------------------------------------------------------------
// Session management
// --------------------------------------------------------------------------

func createSession(c *client, baseURL, providerID string) (string, error) {
	url := baseURL + "/api/sessions"
	req := sessionRequest{
		ProviderType: "openai",
		ProviderID:   providerID,
		Title:        "tooltest",
	}
	resp, err := c.postJSON(url, req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp errorResponse
		_ = decodeJSON(resp.Body, &errResp)
		return "", fmt.Errorf("createSession: HTTP %d — %s %s", resp.StatusCode, errResp.Error, errResp.Details)
	}

	var sess sessionResponse
	if err := decodeJSON(resp.Body, &sess); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	return sess.ID, nil
}

func sendMessage(c *client, baseURL, sessionID, providerID, content string) error {
	url := fmt.Sprintf("%s/api/sessions/%s/messages", baseURL, sessionID)
	req := sendMessageRequest{
		Content:    content,
		ProviderID: providerID,
	}
	resp, err := c.postJSON(url, req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		var errResp errorResponse
		_ = decodeJSON(resp.Body, &errResp)
		return fmt.Errorf("sendMessage: HTTP %d — %s %s", resp.StatusCode, errResp.Error, errResp.Details)
	}
	return nil
}

// --------------------------------------------------------------------------
// SSE streaming
// --------------------------------------------------------------------------

// parseSSEStream reads lines from r and emits complete SSE frames on events.
func parseSSEStream(r io.Reader, events chan<- sseEvent) {
	defer close(events)

	scanner := bufio.NewScanner(r)
	var cur sseEvent
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "id:"):
			cur.id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			cur.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			// Blank line = end of frame
			if cur.event != "" || cur.data != "" {
				events <- cur
			}
			cur = sseEvent{}
		}
	}
}

// --------------------------------------------------------------------------
// Event pretty-printer
// --------------------------------------------------------------------------

func prettyPrint(ev sseEvent) (done bool) {
	if ev.event == "heartbeat" {
		fmt.Printf("%s heartbeat\n", colored(colorGray, "~"))
		return false
	}

	ts := time.Now().Format("15:04:05.000")

	// Parse the JSON data blob.
	var raw map[string]any
	if err := json.Unmarshal([]byte(ev.data), &raw); err != nil {
		fmt.Printf("%s [%s] %s\n", colored(colorGray, ts), ev.event, ev.data)
		return false
	}

	eventType := ev.event
	if eventType == "" {
		if t, ok := raw["type"].(string); ok {
			eventType = t
		}
	}

	switch eventType {
	case "output":
		data := getMap(raw, "data")
		content := getString(data, "content")
		isDelta := getBool(data, "is_delta")
		if isDelta {
			fmt.Printf("%s", colored(colorGreen, content))
		} else {
			fmt.Printf("\n%s [output] %s\n", colored(colorGray, ts), colored(colorGreen, content))
		}

	case "tool_call":
		data := getMap(raw, "data")
		name := getString(data, "name")
		status := getString(data, "status")
		id := getString(data, "id")
		inputJSON, _ := json.MarshalIndent(data["input"], "    ", "  ")
		outputJSON, _ := json.MarshalIndent(data["output"], "    ", "  ")

		switch status {
		case "started", "":
			fmt.Printf("\n%s %s tool_call %s [%s]\n",
				colored(colorGray, ts),
				colored(colorYellow, ">>>"),
				colored(colorBold, name),
				colored(colorGray, id),
			)
			if len(inputJSON) > 2 {
				fmt.Printf("    input: %s\n", string(inputJSON))
			}
		case "completed":
			fmt.Printf("%s %s tool_call %s [%s] completed\n",
				colored(colorGray, ts),
				colored(colorGreen, "<<<"),
				colored(colorBold, name),
				colored(colorGray, id),
			)
			if len(outputJSON) > 2 {
				fmt.Printf("    output: %s\n", string(outputJSON))
			}
		default:
			fmt.Printf("%s [tool_call/%s] %s id=%s\n",
				colored(colorGray, ts), status, colored(colorBold, name), id)
		}

	case "status_change":
		data := getMap(raw, "data")
		oldState := getString(data, "old_state")
		newState := getString(data, "new_state")
		reason := getString(data, "reason")

		arrow := "-->"
		stateColor := colorCyan
		switch newState {
		case "suspended":
			stateColor = colorYellow
			arrow = "!!>"
		case "idle":
			stateColor = colorGreen
			arrow = "==>"
		case "running":
			stateColor = colorCyan
		}

		msg := fmt.Sprintf("%s %s %s %s %s",
			colored(colorGray, ts),
			colored(colorBold, "state"),
			oldState,
			colored(colorBold, arrow),
			colored(stateColor, newState),
		)
		if reason != "" {
			msg += fmt.Sprintf(" (%s)", colored(colorGray, reason))
		}
		fmt.Println(msg)

		if newState == "idle" {
			return true
		}

	case "thought":
		data := getMap(raw, "data")
		content := getString(data, "content")
		// Truncate long thoughts for readability.
		if len(content) > 200 {
			content = content[:200] + "…"
		}
		fmt.Printf("%s %s %s\n", colored(colorGray, ts), colored(colorGray, "[thought]"), colored(colorGray, content))

	case "error":
		data := getMap(raw, "data")
		msg := getString(data, "message")
		code := getString(data, "code")
		fmt.Printf("%s %s %s (code=%s)\n",
			colored(colorGray, ts),
			colored(colorRed, "[ERROR]"),
			msg, code)

	case "metric":
		data := getMap(raw, "data")
		tokIn := getFloat(data, "tokens_in")
		tokOut := getFloat(data, "tokens_out")
		reqs := getFloat(data, "request_count")
		fmt.Printf("%s %s in=%d out=%d reqs=%d\n",
			colored(colorGray, ts),
			colored(colorGray, "[metric]"),
			int(tokIn), int(tokOut), int(reqs))

	case "user_message":
		data := getMap(raw, "data")
		content := getString(data, "content")
		fmt.Printf("%s %s %s\n", colored(colorGray, ts), colored(colorCyan, "[user]"), content)

	case "system_message":
		data := getMap(raw, "data")
		content := getString(data, "content")
		if len(content) > 120 {
			content = content[:120] + "…"
		}
		fmt.Printf("%s %s %s\n", colored(colorGray, ts), colored(colorGray, "[system]"), content)

	case "plan":
		data := getMap(raw, "data")
		desc := getString(data, "description")
		fmt.Printf("%s %s %s\n", colored(colorGray, ts), colored(colorCyan, "[plan]"), desc)

	case "metadata":
		data := getMap(raw, "data")
		key := getString(data, "key")
		val := data["value"]
		fmt.Printf("%s %s %s = %v\n", colored(colorGray, ts), colored(colorGray, "[meta]"), key, val)

	default:
		fmt.Printf("%s [%s] %s\n", colored(colorGray, ts), colored(colorGray, eventType), ev.data)
	}

	return false
}

// --------------------------------------------------------------------------
// JSON helpers
// --------------------------------------------------------------------------

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return map[string]any{}
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// --------------------------------------------------------------------------
// Main
// --------------------------------------------------------------------------

func main() {
	var (
		flagSession  = flag.String("session", "", "Use existing session ID (creates new if not set)")
		flagProvider = flag.String("provider", "prov_8ee0342abddb8e21", "Provider ID to use")
		flagMessage  = flag.String("message", "Please read the file /etc/hostname using the read_file tool", "Message to send")
		flagURL      = flag.String("url", "http://localhost:8080", "Base URL of the orbitmesh instance")
		flagWatch    = flag.Bool("watch", false, "Just watch events on an existing session (don't send a message)")
	)
	flag.Parse()

	baseURL := strings.TrimRight(*flagURL, "/")

	// ---- 0. Bootstrap HTTP client with CSRF token ----------------------------
	hc, err := newClient(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing HTTP client: %v\n", err)
		os.Exit(1)
	}

	// ---- 1. Create or reuse session ------------------------------------------
	sessionID := *flagSession
	if sessionID == "" {
		fmt.Printf("%s Creating new session (provider: %s) …\n",
			colored(colorCyan, "[tooltest]"), *flagProvider)
		id, err := createSession(hc, baseURL, *flagProvider)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
			os.Exit(1)
		}
		sessionID = id
		fmt.Printf("%s Session created: %s\n", colored(colorCyan, "[tooltest]"), colored(colorBold, sessionID))
	} else {
		fmt.Printf("%s Using existing session: %s\n", colored(colorCyan, "[tooltest]"), colored(colorBold, sessionID))
	}

	// ---- 2. Connect to SSE stream (before sending message to avoid race) ------
	sseURL := fmt.Sprintf("%s/api/sessions/%s/events", baseURL, sessionID)
	fmt.Printf("%s Connecting to SSE stream: %s\n", colored(colorCyan, "[tooltest]"), sseURL)

	httpClient := &http.Client{Timeout: 0} // no timeout — we stream
	req, err := http.NewRequest(http.MethodGet, sseURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building SSE request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	sseResp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to SSE stream: %v\n", err)
		os.Exit(1)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(sseResp.Body)
		fmt.Fprintf(os.Stderr, "SSE stream returned HTTP %d: %s\n", sseResp.StatusCode, string(body))
		os.Exit(1)
	}
	fmt.Printf("%s SSE stream connected.\n\n", colored(colorCyan, "[tooltest]"))

	// Start streaming SSE into a channel in the background.
	events := make(chan sseEvent, 64)
	go parseSSEStream(sseResp.Body, events)

	// ---- 3. Send message (unless -watch) -------------------------------------
	if !*flagWatch {
		fmt.Printf("%s Sending message: %q\n\n", colored(colorCyan, "[tooltest]"), *flagMessage)
		if err := sendMessage(hc, baseURL, sessionID, *flagProvider, *flagMessage); err != nil {
			fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
			os.Exit(1)
		}
	}

	// ---- 4. Consume and pretty-print events ----------------------------------
	timeout := time.After(120 * time.Second)
	for {
		select {
		case <-timeout:
			fmt.Printf("\n%s Timeout (120 s) reached — stopping.\n", colored(colorYellow, "[tooltest]"))
			return

		case ev, ok := <-events:
			if !ok {
				fmt.Printf("\n%s SSE stream closed.\n", colored(colorCyan, "[tooltest]"))
				return
			}
			done := prettyPrint(ev)
			if done {
				fmt.Printf("\n%s Session returned to idle — done.\n", colored(colorGreen, "[tooltest]"))
				return
			}
		}
	}
}
