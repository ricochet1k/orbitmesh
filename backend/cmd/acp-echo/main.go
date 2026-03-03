package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type config struct {
	mode           string
	controlAddr    string
	transcriptPath string
	sdkURL         string
}

func parseConfig() config {
	cfg := config{}
	flag.StringVar(&cfg.mode, "mode", "acp", "mock mode: acp, claude-stdio, claudews, codex-app-server")
	flag.StringVar(&cfg.controlAddr, "control-addr", "", "optional localhost control HTTP bind address (for example 127.0.0.1:18080)")
	flag.StringVar(&cfg.transcriptPath, "transcript-path", "", "optional NDJSON transcript output path")
	flag.StringVar(&cfg.sdkURL, "sdk-url", "", "ClaudeWS SDK websocket URL (required in --mode claudews)")
	flag.Parse()
	return cfg
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := parseConfig()

	recorder, err := newTranscriptRecorder(cfg.transcriptPath)
	if err != nil {
		fatalf("transcript setup failed: %v", err)
	}
	defer recorder.Close()

	controller := newController(recorder)
	if cfg.controlAddr != "" {
		if err := controller.Start(cfg.controlAddr); err != nil {
			fatalf("control server failed to start: %v", err)
		}
		defer controller.Close()
		fmt.Fprintf(os.Stderr, "acp-echo control listening on http://%s\n", controller.Addr())
	}

	switch cfg.mode {
	case "acp":
		err = runACPMode(ctx, controller, recorder)
	case "claude-stdio":
		err = runClaudeStdioMode(ctx, os.Stdin, os.Stdout, controller, recorder)
	case "claudews":
		err = runClaudeWSMode(ctx, cfg.sdkURL, controller, recorder)
	case "codex-app-server":
		err = runCodexAppServerMode(ctx, os.Stdin, os.Stdout, controller, recorder)
	default:
		err = fmt.Errorf("unknown mode %q", cfg.mode)
	}

	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "acp-echo: "+format+"\n", args...)
	os.Exit(1)
}
