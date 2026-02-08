import type { SimulationNodeDatum, SimulationLinkDatum } from "d3";

export type GraphNodeType = "agent" | "task" | "commit";

export interface GraphNode {
  id: string;
  type: GraphNodeType;
  label: string;
  status?: string;
  /** Tree depth: 0 = root, 1 = first children, etc. */
  depth?: number;
  /** Priority: higher values bias the node towards the top of the layout. */
  priority?: number;
}

/** Mutable simulation node used internally by d3. */
export interface SimNode extends GraphNode, SimulationNodeDatum {}

/** Link as stored in props (string ids). */
export interface GraphLink {
  source: string;
  target: string;
}

/** Link after d3 resolves ids to node references. */
export type SimLink = SimulationLinkDatum<SimNode>;
