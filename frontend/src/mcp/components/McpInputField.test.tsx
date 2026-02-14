import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { McpInputField } from "./McpInputField";
import { mcpDispatch } from "../dispatch";

describe("McpInputField", () => {
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

  it("edits input value via MCP action", async () => {
    const onInput = vi.fn();
    const { unmount } = render(() => (
      <McpInputField
        mcpId="field-1"
        mcpName="Field"
        mcpDescription="Editable field"
        data-testid="mcp-input"
        onInput={onInput}
      />
    ));

    const input = screen.getByTestId("mcp-input") as HTMLInputElement;
    expect(input.value).toBe("");

    const resultPromise = mcpDispatch.dispatchAction("field-1", "edit", { value: "hello" });
    vi.runAllTimers();
    const result = await resultPromise;

    expect(result.ok).toBe(true);
    expect(input.value).toBe("hello");
    expect(onInput).toHaveBeenCalled();

    unmount();
  });
});
