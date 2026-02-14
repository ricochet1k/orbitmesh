import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { McpButton } from "./McpButton";
import { mcpDispatch } from "../dispatch";

describe("McpButton", () => {
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

  it("fires click handler via MCP action", async () => {
    const onClick = vi.fn();
    const { unmount } = render(() => (
      <McpButton
        mcpId="button-1"
        mcpName="Button"
        mcpDescription="Clickable button"
        onClick={onClick}
      >
        Press
      </McpButton>
    ));

    const button = screen.getByRole("button", { name: "Press" });
    expect(button).toBeDefined();

    const resultPromise = mcpDispatch.dispatchAction("button-1", "click", undefined);
    vi.runAllTimers();
    const result = await resultPromise;

    expect(result.ok).toBe(true);
    expect(onClick).toHaveBeenCalled();

    unmount();
  });
});
