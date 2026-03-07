// Package mcpclient provides an MCP client that can connect to external MCP
// servers over stdio (subprocess) or SSE (HTTP) transports and proxy their
// tools through the OrbitMesh MCP gateway.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// Client wraps an mcp.ClientSession and exposes the capabilities we need.
type Client struct {
	session *mcp.ClientSession
	cancel  context.CancelFunc
}

// Connect dials the given MCP server entry and initialises the MCP handshake.
// Call Close when done.
func Connect(ctx context.Context, entry storage.MCPServerEntry) (*Client, error) {
	c := mcp.NewClient(&mcp.Implementation{Name: "orbitmesh", Version: "1.0.0"}, nil)

	clientCtx, cancel := context.WithCancel(ctx)

	var transport mcp.Transport
	switch entry.Type {
	case "command":
		if entry.Command == "" {
			cancel()
			return nil, fmt.Errorf("mcpclient: command type requires a command")
		}
		parts := strings.Fields(entry.Command)
		cmd := exec.CommandContext(clientCtx, parts[0], append(parts[1:], entry.Args...)...)
		for k, v := range entry.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		transport = &mcp.CommandTransport{Command: cmd}

	case "sse":
		if entry.URL == "" {
			cancel()
			return nil, fmt.Errorf("mcpclient: sse type requires a url")
		}
		transport = &mcp.SSEClientTransport{
			Endpoint:   entry.URL,
			HTTPClient: httpClientForEntry(entry),
		}

	default:
		cancel()
		return nil, fmt.Errorf("mcpclient: unknown type %q", entry.Type)
	}

	session, err := c.Connect(clientCtx, transport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcpclient: connect to %q: %w", entry.Name, err)
	}

	return &Client{session: session, cancel: cancel}, nil
}

// ListTools returns all tools advertised by the server.
func (c *Client) ListTools(ctx context.Context) ([]apiTypes.MCPToolInfo, error) {
	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]apiTypes.MCPToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema, _ := json.Marshal(t.InputSchema)
		out = append(out, apiTypes.MCPToolInfo{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: json.RawMessage(schema),
		})
	}
	return out, nil
}

// ListResources returns all resources advertised by the server.
func (c *Client) ListResources(ctx context.Context) ([]apiTypes.MCPResourceInfo, error) {
	res, err := c.session.ListResources(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]apiTypes.MCPResourceInfo, 0, len(res.Resources))
	for _, r := range res.Resources {
		out = append(out, apiTypes.MCPResourceInfo{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MIMEType,
		})
	}
	return out, nil
}

// ListPrompts returns all prompts advertised by the server.
func (c *Client) ListPrompts(ctx context.Context) ([]apiTypes.MCPPromptInfo, error) {
	res, err := c.session.ListPrompts(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]apiTypes.MCPPromptInfo, 0, len(res.Prompts))
	for _, p := range res.Prompts {
		out = append(out, apiTypes.MCPPromptInfo{
			Name:        p.Name,
			Description: p.Description,
		})
	}
	return out, nil
}

// CallTool invokes a tool on the server and returns the text result.
// isError is true when the server reports the call failed at the tool level.
func (c *Client) CallTool(ctx context.Context, name string, input json.RawMessage) (string, bool, error) {
	var args any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", false, fmt.Errorf("mcpclient: unmarshal args: %w", err)
		}
	}

	res, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", false, err
	}

	// Collect text from all content items.
	var sb strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError, nil
}

// Close terminates the connection to the MCP server.
func (c *Client) Close() {
	c.cancel()
	_ = c.session.Close()
}
