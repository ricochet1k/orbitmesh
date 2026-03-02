package claudews

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

const (
	customDiagnosticsEnabledKey  = "claudews_diagnostics_enabled"
	customDiagnosticsDirKey      = "claudews_diagnostics_dir"
	customTranscriptPathKey      = "claudews_diagnostics_transcript_path"
	customStdoutPathKey          = "claudews_diagnostics_stdout_path"
	customStderrPathKey          = "claudews_diagnostics_stderr_path"
	defaultTranscriptFilename    = "ws_transcript.ndjson"
	defaultStdoutDiagnosticsFile = "stdout.ndjson"
	defaultStderrDiagnosticsFile = "stderr.ndjson"
	defaultDiagnosticsTempDir    = "orbitmesh-claudews-diagnostics"
)

type diagnosticsPaths struct {
	Transcript string
	Stdout     string
	Stderr     string
}

type diagnosticsRecorder struct {
	mu         sync.Mutex
	paths      diagnosticsPaths
	transcript *os.File
	stdout     *os.File
	stderr     *os.File
}

type diagnosticsRecord struct {
	Timestamp string `json:"ts"`
	Kind      string `json:"kind"`
	Payload   any    `json:"payload"`
}

func newDiagnosticsRecorder(config session.Config, sessionID string) (*diagnosticsRecorder, error) {
	paths, enabled := resolveDiagnosticsPaths(config, sessionID)
	if !enabled {
		return nil, nil
	}

	openFile := func(path string) (*os.File, error) {
		if strings.TrimSpace(path) == "" {
			return nil, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create diagnostics directory for %s: %w", path, err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open diagnostics file %s: %w", path, err)
		}
		return f, nil
	}

	transcript, err := openFile(paths.Transcript)
	if err != nil {
		return nil, err
	}
	stdout, err := openFile(paths.Stdout)
	if err != nil {
		if transcript != nil {
			_ = transcript.Close()
		}
		return nil, err
	}
	stderr, err := openFile(paths.Stderr)
	if err != nil {
		if transcript != nil {
			_ = transcript.Close()
		}
		if stdout != nil {
			_ = stdout.Close()
		}
		return nil, err
	}

	return &diagnosticsRecorder{
		paths:      paths,
		transcript: transcript,
		stdout:     stdout,
		stderr:     stderr,
	}, nil
}

func resolveDiagnosticsPaths(config session.Config, sessionID string) (diagnosticsPaths, bool) {
	if config.Custom == nil {
		return diagnosticsPaths{}, false
	}

	enabled := customBool(config.Custom, customDiagnosticsEnabledKey)
	dir := customString(config.Custom, customDiagnosticsDirKey)
	transcript := customString(config.Custom, customTranscriptPathKey)
	stdout := customString(config.Custom, customStdoutPathKey)
	stderr := customString(config.Custom, customStderrPathKey)

	if !enabled && dir == "" && transcript == "" && stdout == "" && stderr == "" {
		return diagnosticsPaths{}, false
	}

	if dir == "" {
		dir = filepath.Join(os.TempDir(), defaultDiagnosticsTempDir, sanitizeDiagnosticsPathPart(sessionID))
	}
	if transcript == "" {
		transcript = filepath.Join(dir, defaultTranscriptFilename)
	}
	if stdout == "" {
		stdout = filepath.Join(dir, defaultStdoutDiagnosticsFile)
	}
	if stderr == "" {
		stderr = filepath.Join(dir, defaultStderrDiagnosticsFile)
	}

	return diagnosticsPaths{Transcript: transcript, Stdout: stdout, Stderr: stderr}, true
}

func sanitizeDiagnosticsPathPart(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return "session"
	}
	clean = strings.ReplaceAll(clean, string(filepath.Separator), "_")
	return clean
}

func customString(custom map[string]any, key string) string {
	raw, ok := custom[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func customBool(custom map[string]any, key string) bool {
	raw, ok := custom[key]
	if !ok {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func (r *diagnosticsRecorder) Paths() diagnosticsPaths {
	if r == nil {
		return diagnosticsPaths{}
	}
	return r.paths
}

func (r *diagnosticsRecorder) RecordLifecycle(kind string, payload any) {
	r.write(r.transcript, kind, payload)
}

func (r *diagnosticsRecorder) RecordWSInbound(payload []byte) {
	r.write(r.transcript, "ws_inbound", map[string]any{"payload": string(payload)})
}

func (r *diagnosticsRecorder) RecordWSOutbound(payload []byte) {
	r.write(r.transcript, "ws_outbound", map[string]any{"payload": string(payload)})
}

func (r *diagnosticsRecorder) RecordStdout(payload string) {
	r.write(r.stdout, "stdout_chunk", map[string]any{"payload": payload})
}

func (r *diagnosticsRecorder) RecordStderr(payload string) {
	r.write(r.stderr, "stderr_chunk", map[string]any{"payload": payload})
}

func (r *diagnosticsRecorder) write(target *os.File, kind string, payload any) {
	if r == nil || target == nil {
		return
	}
	record := diagnosticsRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Kind:      kind,
		Payload:   payload,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = target.Write(append(line, '\n'))
}

func (r *diagnosticsRecorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.transcript != nil {
		_ = r.transcript.Close()
		r.transcript = nil
	}
	if r.stdout != nil {
		_ = r.stdout.Close()
		r.stdout = nil
	}
	if r.stderr != nil {
		_ = r.stderr.Close()
		r.stderr = nil
	}
}
