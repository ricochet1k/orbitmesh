package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func maybeRunMCPBridge() bool {
	if len(os.Args) < 2 || os.Args[1] != "mcp-bridge" {
		return false
	}
	if err := runMCPBridge(context.Background(), os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-bridge: %v\n", err)
		os.Exit(1)
	}
	return true
}

func runMCPBridge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("mcp-bridge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sessionID := fs.String("session-id", "", "OrbitMesh session ID")
	apiBaseURL := fs.String("api-base-url", "", "OrbitMesh API base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*sessionID) == "" {
		return errors.New("--session-id is required")
	}

	baseURL := strings.TrimSpace(*apiBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ORBITMESH_API_BASE_URL"))
	}
	if baseURL == "" {
		port := strings.TrimSpace(os.Getenv("ORBITMESH_PORT"))
		if port == "" {
			port = "8080"
		}
		baseURL = "http://127.0.0.1:" + strings.TrimPrefix(port, ":")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/sessions/%s/mcp/ws-token", baseURL, *sessionID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Orbitmesh-Internal", "frontend-tools-mcp")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("token request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token apiTypes.MCPGatewayTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, token.WSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	errCh := make(chan error, 2)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, readErr := reader.ReadBytes('\n')
			if len(line) > 0 {
				if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
					errCh <- err
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					errCh <- nil
					return
				}
				errCh <- readErr
				return
			}
		}
	}()

	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}
			msg = bytes.TrimSpace(msg)
			if len(msg) == 0 {
				continue
			}
			if _, err := os.Stdout.Write(append(msg, '\n')); err != nil {
				errCh <- err
				return
			}
		}
	}()

	if err := <-errCh; err != nil {
		return err
	}
	return nil
}
