export type GraphNodeType = "agent" | "task" | "commit";

export interface GraphNode {
  id: string;
  type: GraphNodeType;
  label: string;
  status?: string;
}

export interface GraphLink {
  source: string;
  target: string;
}
