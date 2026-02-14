import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { createMcpDispatch } from "./dispatch";
import { createMcpRegistry } from "./registry";

describe("createMcpDispatch", () => {
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

  it("returns an error when the component is missing", async () => {
    const dispatch = createMcpDispatch(createMcpRegistry());

    const result = await dispatch.dispatchAction("missing", "click", undefined);

    expect(result.ok).toBe(false);
    expect(result.error).toBe("Unknown MCP component");
  });

  it("runs animation before invoking the handler", async () => {
    const registry = createMcpRegistry();
    const dispatch = createMcpDispatch(registry);
    const button = document.createElement("button");
    const order: string[] = [];
    let hadPulse = false;

    button.scrollIntoView = vi.fn(() => order.push("scroll"));

    registry.register({
      id: "btn-1",
      name: "Button",
      description: "Test button",
      element: button,
      actions: {
        click: () => {
          hadPulse = button.classList.contains("mcp-pulse");
          order.push("handler");
          return { ok: true };
        },
      },
    });

    const resultPromise = dispatch.dispatchAction("btn-1", "click", undefined);
    await vi.advanceTimersByTimeAsync(0);
    const result = await resultPromise;

    expect(result.ok).toBe(true);
    expect(hadPulse).toBe(true);
    expect(order).toEqual(["scroll", "handler"]);
  });

  it("returns per-field results for multi-field edit", async () => {
    const registry = createMcpRegistry();
    const dispatch = createMcpDispatch(registry);
    const updates: Record<string, string> = {};

    registry.register({
      id: "field-1",
      name: "Field 1",
      description: "First field",
      element: document.createElement("input"),
      actions: {
        edit: (payload: { value: string }) => {
          updates["field-1"] = payload.value;
          return { ok: true };
        },
      },
    });

    registry.register({
      id: "field-2",
      name: "Field 2",
      description: "Second field",
      element: document.createElement("input"),
      actions: {
        edit: () => ({ ok: false, error: "Edit failed" }),
      },
    });

    const resultPromise = dispatch.dispatchMultiFieldEdit({
      "field-1": 42,
      "field-2": "bad",
    });
    await vi.runAllTimersAsync();
    const result = await resultPromise;

    expect(result.ok).toBe(false);
    expect(result.results["field-1"]).toEqual({ ok: true, error: undefined });
    expect(result.results["field-2"]).toEqual({ ok: false, error: "Edit failed" });
    expect(updates["field-1"]).toBe("42");
  });

  it("accepts array payloads for multi-field edit", async () => {
    const registry = createMcpRegistry();
    const dispatch = createMcpDispatch(registry);
    const updates: Record<string, string> = {};
    const order: string[] = [];

    registry.register({
      id: "field-1",
      name: "Field 1",
      description: "First field",
      element: document.createElement("input"),
      actions: {
        edit: (payload: { value: string }) => {
          updates["field-1"] = payload.value;
          order.push("field-1");
          return { ok: true };
        },
      },
    });

    registry.register({
      id: "field-2",
      name: "Field 2",
      description: "Second field",
      element: document.createElement("input"),
      actions: {
        edit: (payload: { value: string }) => {
          updates["field-2"] = payload.value;
          order.push("field-2");
          return { ok: true };
        },
      },
    });

    const resultPromise = dispatch.dispatchMultiFieldEdit([
      { fieldId: "field-2", value: "second" },
      { fieldId: "field-1", value: 3 },
    ]);
    await vi.runAllTimersAsync();
    const result = await resultPromise;

    expect(result.ok).toBe(true);
    expect(result.results["field-2"]).toEqual({ ok: true, error: undefined });
    expect(result.results["field-1"]).toEqual({ ok: true, error: undefined });
    expect(updates["field-2"]).toBe("second");
    expect(updates["field-1"]).toBe("3");
    expect(order).toEqual(["field-2", "field-1"]);
  });
});
