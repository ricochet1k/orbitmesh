package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ricochet1k/orbitmesh/internal/storage"
	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

type Client struct {
	client client.MCPClient
	cancel context.CancelFunc
}

func envVars(env map[string]string) []string {
	var vars []string
	for k, v := range env {
		vars = append(vars, fmt.Sprintf("%s=%s", k, v))
	}
	return vars
}


func Connect(ctx context.Context, entry storage.MCPServerEntry) (*Client, error) {
	clientCtx, cancel := context.WithCancel(ctx)

	var mcpClient client.MCPClient
	var err error

	switch entry.Type {
	case "command":
		if entry.Command == "" {
			cancel()
			return nil, fmt.Errorf("mcpclient: command type requires a command")
		}

		mcpClient, err = client.NewStdioMCPClient(entry.Command, envVars(entry.Env), entry.Args...)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create stdio client: %w", err)
		}
	case "sse":
		if entry.URL == "" {
			cancel()
			return nil, fmt.Errorf("mcpclient: sse type requires a url")
		}

		customHTTPClient := httpClientForEntry(entry)
		mcpClient, err = client.NewSSEMCPClient(entry.URL, client.WithHTTPClient(customHTTPClient))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create sse client: %w", err)
		}
	default:
		cancel()
		return nil, fmt.Errorf("mcpclient: unknown type %q", entry.Type)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "orbitmesh",
		Version: "1.0.0",
	}

	_, err = mcpClient.Initialize(clientCtx, initReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcpclient: initialize %q: %w", entry.Name, err)
	}

	return &Client{client: mcpClient, cancel: cancel}, nil
}


func (c *Client) ListTools(ctx context.Context) ([]apiTypes.MCPToolInfo, error) {
	req := mcp.ListToolsRequest{}
	res, err := c.client.ListTools(ctx, req)
	if err != nil {
		return nil, err
	}

	var out []apiTypes.MCPToolInfo
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

func (c *Client) ListResources(ctx context.Context) ([]apiTypes.MCPResourceInfo, error) {
	req := mcp.ListResourcesRequest{}
	res, err := c.client.ListResources(ctx, req)
	if err != nil {
		return nil, err
	}

	var out []apiTypes.MCPResourceInfo
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

func (c *Client) ListPrompts(ctx context.Context) ([]apiTypes.MCPPromptInfo, error) {
	req := mcp.ListPromptsRequest{}
	res, err := c.client.ListPrompts(ctx, req)
	if err != nil {
		return nil, err
	}

	var out []apiTypes.MCPPromptInfo
	for _, p := range res.Prompts {
		out = append(out, apiTypes.MCPPromptInfo{
			Name:        p.Name,
			Description: p.Description,
		})
	}
	return out, nil
}

func (c *Client) CallTool(ctx context.Context, name string, input json.RawMessage) (string, bool, error) {
	var args map[string]interface{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", false, fmt.Errorf("mcpclient: unmarshal args: %w", err)
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	res, err := c.client.CallTool(ctx, req)
	if err != nil {
		return "", false, err
	}

	var sb strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError, nil
}

func (c *Client) Close() {
	if c.client != nil {
		c.client.Close()
	}
	c.cancel()
}
