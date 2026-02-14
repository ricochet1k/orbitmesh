import { createEffect, createMemo, createUniqueId } from "solid-js";
import type { JSX } from "solid-js";
import type { McpActionHandlers, McpMetadataProps } from "../contract";
import { mcpRegistry } from "../registry";

type McpInputFieldProps = JSX.InputHTMLAttributes<HTMLInputElement> &
  Partial<McpMetadataProps>;

const buildActions = (
  input: () => HTMLInputElement | undefined,
  overrides?: McpActionHandlers,
): McpActionHandlers => ({
  edit: (payload: { value: string }) => {
    const target = input();
    if (!target) return { ok: false, error: "Input is unavailable" };
    target.value = payload?.value ?? "";
    target.dispatchEvent(new Event("input", { bubbles: true }));
    target.dispatchEvent(new Event("change", { bubbles: true }));
    return { ok: true, data: target.value };
  },
  focus: () => {
    const target = input();
    if (!target) return { ok: false, error: "Input is unavailable" };
    target.focus();
    return { ok: true };
  },
  ...overrides,
});

export const McpInputField = (props: McpInputFieldProps) => {
  const id = createUniqueId();
  let inputRef: HTMLInputElement | undefined;
  const metadata = createMemo(() => ({
    id: props.mcpId ?? `mcp-input-${id}`,
    name: props.mcpName,
    description: props.mcpDescription,
  }));
  const isEnabled = () => Boolean(metadata().name && metadata().description);

  createEffect(() => {
    if (!isEnabled() || !inputRef) return;
    const entry = {
      id: metadata().id,
      name: metadata().name as string,
      description: metadata().description as string,
      element: inputRef,
      actions: buildActions(() => inputRef, props.mcpActions),
      getValue: () => inputRef?.value ?? "",
    };
    mcpRegistry.register(entry);
    return () => mcpRegistry.unregister(entry.id);
  });

  const { mcpActions, mcpDescription, mcpId, mcpName, ...inputProps } = props;

  return <input ref={inputRef} {...inputProps} />;
};
