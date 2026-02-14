import type { Accessor } from "solid-js";
import type { McpActionHandlers } from "./contract";

export type McpRegistryEntry = {
  id: string;
  name: string;
  description: string;
  element: HTMLElement;
  actions: McpActionHandlers;
  getValue?: Accessor<unknown>;
};

export type McpRegistry = {
  register: (entry: McpRegistryEntry) => void;
  unregister: (id: string) => void;
  get: (id: string) => McpRegistryEntry | undefined;
  list: () => McpRegistryEntry[];
};

export const createMcpRegistry = (): McpRegistry => {
  const entries = new Map<string, McpRegistryEntry>();

  return {
    register: (entry) => {
      entries.set(entry.id, entry);
    },
    unregister: (id) => {
      entries.delete(id);
    },
    get: (id) => entries.get(id),
    list: () => Array.from(entries.values()),
  };
};

export const mcpRegistry = createMcpRegistry();
