import { fireEvent, render, screen } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import MCPServersRoute from "./mcp-servers";

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
}));

vi.mock("../../api/client", () => ({
  apiClient: {
    listMCPServers: vi.fn().mockResolvedValue({
      servers: [
        {
          id: "server-1",
          name: "filesystem",
          type: "command",
          command: "npx @modelcontextprotocol/server-filesystem",
          auth_type: "none",
          has_token: false,
          dynamic_registration: false,
        },
      ],
    }),
    createMCPServer: vi.fn(),
    updateMCPServer: vi.fn(),
    deleteMCPServer: vi.fn(),
    startMCPOAuth: vi.fn(),
    getMCPServer: vi.fn(),
    revokeMCPOAuthToken: vi.fn(),
    getMCPServerCapabilities: vi.fn().mockResolvedValue({
      tools: [],
      resources: [],
      prompts: [],
    }),
  },
}));

describe("MCP servers route smoke", () => {
  it("hides advanced MCP controls by default and reveals them with toggle", async () => {
    render(() => <MCPServersRoute />);

    expect(await screen.findByTestId("mcp-servers-page")).toBeDefined();
    expect(await screen.findByText("filesystem")).toBeDefined();
    expect(screen.getByTestId("mcp-tools-collapsed-server-1")).toBeDefined();
    expect(screen.queryByRole("button", { name: "View Tools" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));

    expect(await screen.findByRole("button", { name: "View Tools" })).toBeDefined();
    expect(screen.queryByTestId("mcp-tools-collapsed-server-1")).toBeNull();
  });
});
