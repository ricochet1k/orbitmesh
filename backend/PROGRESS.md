# MCP Proxy Implementation Progress

## Backend (DONE - compiles clean)

- [x] Create MCPServerRegistry storage (`internal/storage/mcp_server_registry.go`)
- [x] Extend AgentConfig with MCPServerRefs (`internal/storage/agent_config.go`)
- [x] Add MCP server API types (`pkg/api/types.go`)
- [x] Create mcpclient package (`internal/mcpclient/client.go`, `auth.go`, `oauth.go`)
- [x] Create API handlers (`internal/api/mcp_servers.go`)
- [x] Update agents.go MCPServerRefs handling
- [x] Add MCP server routes to handler.go Mount()
- [x] Enhance gateway with proxy resolver (`internal/mcpws/gateway.go`, `proxy_resolver.go`)
- [x] Wire everything in main.go

## Frontend (DONE - builds clean, 221 tests pass)

- [x] Add MCP server types to `src/types/api.ts`
- [x] Create `src/api/mcpServers.ts` API module
- [x] Register mcpServers in `src/api/client.ts`
- [x] Create `src/routes/settings/mcp-servers.tsx` settings page
- [x] Enhance agent form with MCP server refs (`src/routes/settings/agents.tsx`)
- [x] Add nav link in `src/routes/settings.tsx`
