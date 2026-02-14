import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { McpDataText } from "./McpDataText";
import { mcpDispatch } from "../dispatch";

describe("McpDataText", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      return window.setTimeout(() => cb(0), 0);
    });
  });

  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("returns text value via MCP read action", async () => {
    const { unmount } = render(() => (
      <McpDataText
        mcpId="data-1"
        mcpName="Data"
        mcpDescription="Read-only field"
        value="Alpha"
      />
    ));

    const text = screen.getByText("Alpha");
    expect(text).toBeDefined();

    const resultPromise = mcpDispatch.dispatchAction("data-1", "read", undefined);
    vi.runAllTimers();
    const result = await resultPromise;

    expect(result.ok).toBe(true);
    expect(result.data).toBe("Alpha");

    unmount();
  });
});
