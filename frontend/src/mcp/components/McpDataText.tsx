import { createEffect, createMemo, createUniqueId } from "solid-js";
import type { JSX } from "solid-js";
import type { McpActionHandlers, McpMetadataProps } from "../contract";
import { mcpRegistry } from "../registry";

type McpDataTextProps = JSX.HTMLAttributes<HTMLSpanElement> &
  Partial<McpMetadataProps> & {
    value?: string;
  };

const buildActions = (
  getText: () => string,
  overrides?: McpActionHandlers,
): McpActionHandlers => ({
  read: () => ({ ok: true, data: getText() }),
  ...overrides,
});

export const McpDataText = (props: McpDataTextProps) => {
  const id = createUniqueId();
  let textRef: HTMLSpanElement | undefined;
  const metadata = createMemo(() => ({
    id: props.mcpId ?? `mcp-data-${id}`,
    name: props.mcpName,
    description: props.mcpDescription,
  }));
  const isEnabled = () => Boolean(metadata().name && metadata().description);

  const getText = () => props.value ?? textRef?.textContent ?? "";

  createEffect(() => {
    if (!isEnabled() || !textRef) return;
    const entry = {
      id: metadata().id,
      name: metadata().name as string,
      description: metadata().description as string,
      element: textRef,
      actions: buildActions(getText, props.mcpActions),
      getValue: getText,
    };
    mcpRegistry.register(entry);
    return () => mcpRegistry.unregister(entry.id);
  });

  const { mcpActions, mcpDescription, mcpId, mcpName, value, ...spanProps } = props;

  return (
    <span ref={textRef} {...spanProps}>
      {value ?? props.children}
    </span>
  );
};
