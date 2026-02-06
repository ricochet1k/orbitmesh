import { createEffect, onCleanup } from "solid-js";
import * as d3 from "d3";
import type { GraphLink, GraphNode } from "./types";

interface AgentGraphProps {
  nodes?: GraphNode[];
  links?: GraphLink[];
  selectedId?: string;
  onSelect?: (node: GraphNode) => void;
}

const defaultNodes: GraphNode[] = [
  { id: "agent-1", type: "agent", label: "Agent 1" },
  { id: "task-1", type: "task", label: "Initial Setup" },
  { id: "task-2", type: "task", label: "API Layer" },
];

const defaultLinks: GraphLink[] = [
  { source: "agent-1", target: "task-2" },
];

export default function AgentGraph(props: AgentGraphProps) {
  let svgRef: SVGSVGElement | undefined;

  createEffect(() => {
    if (!svgRef) return;

    const width = svgRef.clientWidth;
    const height = svgRef.clientHeight;

    const svg = d3.select(svgRef);
    svg.selectAll("*").remove();

    const nodes: GraphNode[] = (props.nodes && props.nodes.length > 0)
      ? props.nodes.map((node) => ({ ...node }))
      : defaultNodes.map((node) => ({ ...node }));
    const links: GraphLink[] = (props.links && props.links.length > 0)
      ? props.links.map((link) => ({ ...link }))
      : defaultLinks.map((link) => ({ ...link }));

    if (nodes.length === 0) return;

    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force("link", d3.forceLink<GraphNode, GraphLink>(links).id(d => d.id))
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(42));

    const link = svg.append("g")
      .attr("stroke", "var(--graph-link)")
      .attr("stroke-opacity", 0.7)
      .selectAll("line")
      .data(links)
      .join("line")
      .attr("stroke-width", 2);

    const node = svg.append("g")
      .attr("stroke", "var(--graph-stroke)")
      .attr("stroke-width", 1.2)
      .selectAll("g")
      .data(nodes)
      .join("g")
      .call(d3.drag<any, GraphNode>()
        .on("start", dragstarted)
        .on("drag", dragged)
        .on("end", dragended))
      .on("click", (_, d) => props.onSelect?.(d));

    node.append("circle")
      .attr("r", d => d.id === props.selectedId ? 18 : 14)
      .attr("fill", d => {
        if (d.type === "agent") return "var(--graph-agent)";
        if (d.type === "commit") return "var(--graph-commit)";
        return "var(--graph-task)";
      })
      .attr("stroke", d => d.id === props.selectedId ? "var(--graph-selected)" : "var(--graph-stroke)")
      .attr("stroke-width", d => d.id === props.selectedId ? 2.4 : 1.2);

    node.append("text")
      .attr("dx", 20)
      .attr("dy", ".35em")
      .attr("stroke", "none")
      .attr("fill", "var(--graph-text)")
      .style("font-weight", d => d.id === props.selectedId ? "600" : "400")
      .text(d => d.label);

    simulation.on("tick", () => {
      link
        .attr("x1", d => (d.source as any).x)
        .attr("y1", d => (d.source as any).y)
        .attr("x2", d => (d.target as any).x)
        .attr("y2", d => (d.target as any).y);

      node.attr("transform", d => `translate(${d.x},${d.y})`);
    });

    function dragstarted(event: any, d: any) {
      if (!event.active) simulation.alphaTarget(0.3).restart();
      d.fx = d.x;
      d.fy = d.y;
    }

    function dragged(event: any, d: any) {
      d.fx = event.x;
      d.fy = event.y;
    }

    function dragended(event: any, d: any) {
      if (!event.active) simulation.alphaTarget(0);
      d.fx = null;
      d.fy = null;
    }

    onCleanup(() => simulation.stop());
  });

  return (
    <svg 
      ref={svgRef} 
      style={{ width: "100%", height: "100%" }}
    />
  );
}
