package claudews

import (
	"path/filepath"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/session"
)

func TestResolveDiagnosticsPaths_DisabledWithoutCustomKeys(t *testing.T) {
	paths, enabled := resolveDiagnosticsPaths(session.Config{SessionCustom: map[string]any{"unused": true}}, "session-1")
	if enabled {
		t.Fatalf("expected diagnostics disabled, got enabled with paths %+v", paths)
	}
}

func TestResolveDiagnosticsPaths_EnabledWithDir(t *testing.T) {
	dir := t.TempDir()
	paths, enabled := resolveDiagnosticsPaths(session.Config{Custom: map[string]any{
		customDiagnosticsEnabledKey: true,
		customDiagnosticsDirKey:     dir,
	}}, "session-1")
	if !enabled {
		t.Fatal("expected diagnostics enabled")
	}
	if paths.Transcript != filepath.Join(dir, defaultTranscriptFilename) {
		t.Fatalf("transcript path = %q", paths.Transcript)
	}
	if paths.Stdout != filepath.Join(dir, defaultStdoutDiagnosticsFile) {
		t.Fatalf("stdout path = %q", paths.Stdout)
	}
	if paths.Stderr != filepath.Join(dir, defaultStderrDiagnosticsFile) {
		t.Fatalf("stderr path = %q", paths.Stderr)
	}
}

func TestResolveDiagnosticsPaths_UsesExplicitPaths(t *testing.T) {
	paths, enabled := resolveDiagnosticsPaths(session.Config{Custom: map[string]any{
		customTranscriptPathKey: "/tmp/claudews/transcript.ndjson",
		customStdoutPathKey:     "/tmp/claudews/stdout.ndjson",
		customStderrPathKey:     "/tmp/claudews/stderr.ndjson",
	}}, "session-1")
	if !enabled {
		t.Fatal("expected diagnostics enabled")
	}
	if paths.Transcript != "/tmp/claudews/transcript.ndjson" {
		t.Fatalf("transcript path = %q", paths.Transcript)
	}
	if paths.Stdout != "/tmp/claudews/stdout.ndjson" {
		t.Fatalf("stdout path = %q", paths.Stdout)
	}
	if paths.Stderr != "/tmp/claudews/stderr.ndjson" {
		t.Fatalf("stderr path = %q", paths.Stderr)
	}
}
