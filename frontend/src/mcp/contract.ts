import type { Accessor } from "solid-js";

export type McpActionType = "click" | "edit" | "focus" | "select" | "toggle" | "read";

export type McpActionKey = McpActionType | (string & {});

export type McpActionResult<TData = unknown> = {
  ok: boolean;
  error?: string;
  data?: TData;
};

export interface McpActionContext<TValue = unknown> {
  id: string;
  name: string;
  description: string;
  getValue?: Accessor<TValue>;
}

export type McpActionHandler<TPayload = void, TResult = unknown> = (
  payload: TPayload,
  context: McpActionContext
) => McpActionResult<TResult> | Promise<McpActionResult<TResult>>;

export interface McpActionPayloads {
  click: void;
  edit: { value: string };
  focus: void;
  select: { value: string | number };
  toggle: { value?: boolean };
  read: void;
}

export type McpActionPayload<K extends McpActionType> = McpActionPayloads[K];

export type McpActionHandlers = {
  [K in McpActionKey]?: McpActionHandler<any, any>;
};

export interface McpMetadataProps {
  mcpName: string;
  mcpDescription: string;
  mcpActions?: McpActionHandlers;
  mcpId?: string;
}

export type McpMultiFieldEditPayload =
  | Array<{ fieldId: string; value: unknown }>
  | Record<string, unknown>;

export type McpMultiFieldEditResult = {
  ok: boolean;
  results: Record<string, { ok: boolean; error?: string }>;
};
