import { createEffect, createMemo, createUniqueId } from "solid-js";
import type { JSX } from "solid-js";
import type { McpActionHandlers, McpMetadataProps } from "../contract";
import { mcpRegistry } from "../registry";

type McpButtonProps = JSX.ButtonHTMLAttributes<HTMLButtonElement> &
  Partial<McpMetadataProps>;

const buildActions = (
  button: () => HTMLButtonElement | undefined,
  overrides?: McpActionHandlers,
): McpActionHandlers => ({
  click: () => {
    const target = button();
    if (!target) return { ok: false, error: "Button is unavailable" };
    if (target.disabled) return { ok: false, error: "Button is disabled" };
    target.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    return { ok: true };
  },
  ...overrides,
});

export const McpButton = (props: McpButtonProps) => {
  const id = createUniqueId();
  let buttonRef: HTMLButtonElement | undefined;
  const metadata = createMemo(() => ({
    id: props.mcpId ?? `mcp-button-${id}`,
    name: props.mcpName,
    description: props.mcpDescription,
  }));
  const isEnabled = () => Boolean(metadata().name && metadata().description);

  createEffect(() => {
    if (!isEnabled() || !buttonRef) return;
    const entry = {
      id: metadata().id,
      name: metadata().name as string,
      description: metadata().description as string,
      element: buttonRef,
      actions: buildActions(() => buttonRef, props.mcpActions),
    };
    mcpRegistry.register(entry);
    return () => mcpRegistry.unregister(entry.id);
  });

  const { mcpActions, mcpDescription, mcpId, mcpName, ...buttonProps } = props;

  return (
    <button ref={buttonRef} {...buttonProps}>
      {props.children}
    </button>
  );
};
