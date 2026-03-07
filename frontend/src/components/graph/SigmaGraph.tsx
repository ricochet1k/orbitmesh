import { createEffect, onCleanup, onMount } from "solid-js";
import Sigma from "sigma";
import Graph from "graphology";
import forceAtlas2 from "graphology-layout-forceatlas2";
import { CodeflowQueryResult } from "../../types/codeflow";

interface SigmaGraphProps {
  data: CodeflowQueryResult;
}

const COLORS: Record<string, string> = {
  File: "#4A5568", // Blue-grey
  Function: "#718096", // Slate
  ExecutionUnit: "#805AD5", // Purple
  Finding: "#E53E3E", // Red
  CallSite: "#38A169", // Green
  APIHandler: "#0BC5EA", // Cyan
  APIRequest: "#DD6B20", // Orange
  default: "#A0AEC0",
};

export function SigmaGraph(props: SigmaGraphProps) {
  let containerRef: HTMLDivElement | undefined;
  let sigmaInstance: Sigma | null = null;
  let graph: Graph | null = null;

  onMount(() => {
    if (!containerRef) return;

    graph = new Graph({ multi: true });

    sigmaInstance = new Sigma(graph, containerRef, {
      minCameraRatio: 0.1,
      maxCameraRatio: 10,
    });

    // Add interactions
    sigmaInstance.on("clickNode", (e) => {
      const nodeId = e.node;
      console.log("Clicked node", nodeId, graph?.getNodeAttributes(nodeId));
      // MVP: In the future this could dispatch an event to expand the node
    });

    onCleanup(() => {
      sigmaInstance?.kill();
      graph?.clear();
    });
  });

  createEffect(() => {
    if (!graph || !sigmaInstance || !props.data) return;

    graph.clear();

    const rows = Array.isArray(props.data.rows) ? props.data.rows : [];

    // Process rows to extract nodes and edges
    // Cypher returns rows which might be nodes or relationships
    rows.forEach(row => {
      Object.values(row).forEach((val: any) => {
        if (!val) return;

        // Looks like a node
        if (val.id && val.labels) {
          const id = String(val.id);
          if (!graph!.hasNode(id)) {
            const primaryLabel = val.labels[0] || "default";
            let displayLabel = primaryLabel;
            if (val.props?.name) displayLabel = val.props.name;
            else if (val.props?.path) displayLabel = val.props.path;
            else if (val.props?.rule_id) displayLabel = val.props.rule_id;

            graph!.addNode(id, {
              x: Math.random() * 10,
              y: Math.random() * 10,
              size: primaryLabel === 'File' ? 15 : 10,
              color: COLORS[primaryLabel] || COLORS.default,
              label: displayLabel,
              ...val.props
            });
          }
        }

        // Looks like an edge
        if (val.id && val.type && val.start_id !== undefined && val.end_id !== undefined) {
          const id = String(val.id);
          const source = String(val.start_id);
          const target = String(val.end_id);

          if (!graph!.hasEdge(id)) {
            // Ensure nodes exist to prevent graphology errors
            if (!graph!.hasNode(source)) {
               graph!.addNode(source, { x: Math.random()*10, y: Math.random()*10, size: 5, color: COLORS.default, label: source });
            }
            if (!graph!.hasNode(target)) {
               graph!.addNode(target, { x: Math.random()*10, y: Math.random()*10, size: 5, color: COLORS.default, label: target });
            }

            graph!.addEdgeWithKey(id, source, target, {
              type: "arrow",
              size: 2,
              color: "#718096",
              label: val.type,
              ...val.props
            });
          }
        }
      });
    });

    // Run layout if we have nodes
    if (graph.order > 0) {
      // Force atlas 2 layout
      forceAtlas2.assign(graph, {
        iterations: 100,
        settings: forceAtlas2.inferSettings(graph)
      });

      sigmaInstance.refresh();
    }
  });

  return (
    <div
      ref={containerRef}
      style={{ width: "100%", height: "100%", "min-height": "400px" }}
    />
  );
}
