import { fireEvent, render, screen } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import ExtractorsRoute from "./extractors";

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
}));

vi.mock("../api/client", () => ({
  apiClient: {
    getExtractorConfig: vi.fn().mockResolvedValue({
      config: {
        profiles: [
          {
            id: "profile-1",
            enabled: true,
            match: { command_regex: "", args_regex: "" },
            rules: [
              {
                id: "rule-1",
                enabled: true,
                trigger: { region_changed: { top: 0, bottom: 1 } },
                extract: { type: "region_text", region: { top: 0, bottom: 1 } },
                emit: { kind: "agent_message", update_window: "recent_open" },
              },
            ],
          },
        ],
      },
      errors: [],
    }),
    validateExtractorConfig: vi.fn(),
    saveExtractorConfig: vi.fn(),
    getTerminalSnapshot: vi.fn(),
    replayExtractor: vi.fn(),
  },
}));

vi.mock("../state/sessions", () => ({
  useSessionStore: () => ({
    sessions: () => [
      {
        id: "sess-1",
        provider_type: "claude-ws",
        state: "running",
      },
    ],
    subscribe: () => undefined,
  }),
}));

describe("Extractors route smoke", () => {
  it("keeps snapshot/replay tooling hidden until advanced mode is enabled", async () => {
    render(() => <ExtractorsRoute />);

    expect(await screen.findByTestId("extractors-heading")).toBeDefined();
    expect(screen.getByTestId("extractors-advanced-collapsed")).toBeDefined();
    expect(screen.queryByTestId("extractors-snapshot-panel")).toBeNull();
    expect(screen.queryByTestId("extractors-replay-panel")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));

    expect(await screen.findByTestId("extractors-snapshot-panel")).toBeDefined();
    expect(screen.getByTestId("extractors-replay-panel")).toBeDefined();
  });
});
