import type {
  McpActionKey,
  McpActionPayloads,
  McpActionResult,
  McpMultiFieldEditPayload,
  McpMultiFieldEditResult,
} from "./contract";
import type { McpRegistry, McpRegistryEntry } from "./registry";
import { mcpRegistry } from "./registry";

const pulseClassName = "mcp-pulse";
const pulseDurationMs = 360;

const normalizeEditPayload = (payload: McpMultiFieldEditPayload) => {
  if (Array.isArray(payload)) {
    return payload.reduce<Record<string, unknown>>((acc, entry) => {
      acc[entry.fieldId] = entry.value;
      return acc;
    }, {});
  }
  return payload;
};

const applyPulse = (element: HTMLElement) => {
  element.classList.remove(pulseClassName);
  void element.offsetWidth;
  element.classList.add(pulseClassName);
  window.setTimeout(() => element.classList.remove(pulseClassName), pulseDurationMs);
};

const runActionAnimation = async (element?: HTMLElement | null) => {
  if (!element) return;
  if (typeof element.scrollIntoView === "function") {
    element.scrollIntoView({ behavior: "smooth", block: "center", inline: "center" });
  }
  applyPulse(element);
  await new Promise<void>((resolve) => {
    requestAnimationFrame(() => resolve());
  });
};

const buildContext = (entry: McpRegistryEntry) => ({
  id: entry.id,
  name: entry.name,
  description: entry.description,
  getValue: entry.getValue,
});

export const createMcpDispatch = (registry: McpRegistry) => {
  const dispatchAction = async <K extends McpActionKey>(
    id: string,
    action: K,
    payload: K extends keyof McpActionPayloads ? McpActionPayloads[K] : unknown,
  ): Promise<McpActionResult> => {
    const entry = registry.get(id);
    if (!entry) {
      return { ok: false, error: "Unknown MCP component" };
    }

    const handler = entry.actions[action];
    if (!handler) {
      return { ok: false, error: `Action '${action}' not supported` };
    }

    await runActionAnimation(entry.element);

    try {
      const result = await handler(payload as never, buildContext(entry));
      if (!result || typeof result.ok !== "boolean") {
        return { ok: false, error: "Action handler returned invalid result" };
      }
      return result;
    } catch (error) {
      const message = error instanceof Error ? error.message : "Action handler failed";
      return { ok: false, error: message };
    }
  };

  const dispatchMultiFieldEdit = async (
    payload: McpMultiFieldEditPayload,
  ): Promise<McpMultiFieldEditResult> => {
    const normalized = normalizeEditPayload(payload);
    const results: Record<string, { ok: boolean; error?: string }> = {};
    let ok = true;

    for (const [fieldId, value] of Object.entries(normalized)) {
      const result = await dispatchAction(fieldId, "edit", {
        value: value === undefined || value === null ? "" : String(value),
      });
      results[fieldId] = { ok: result.ok, error: result.error };
      if (!result.ok) ok = false;
    }

    return { ok, results };
  };

  return { dispatchAction, dispatchMultiFieldEdit };
};

export const mcpDispatch = createMcpDispatch(mcpRegistry);
