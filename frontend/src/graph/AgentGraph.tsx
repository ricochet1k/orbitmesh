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
  let resizeObserver: ResizeObserver | undefined;

  // Get container for size calculations
  const getContainerSize = () => {
    if (!svgRef || !svgRef.parentElement) return { width: 0, height: 0 };
    const rect = svgRef.parentElement.getBoundingClientRect();
    return { 
      width: rect.width || 800, 
      height: rect.height || 500 
    };
  };

  const renderGraph = () => {
    if (!svgRef) return;

    const { width, height } = getContainerSize();
    
    // Ensure minimum dimensions
    if (width < 100 || height < 100) return;

    const svg = d3.select(svgRef);
    
    // Set SVG dimensions explicitly
    svg.attr("width", width).attr("height", height);
    svg.selectAll("*").remove();

    const nodes: GraphNode[] = (props.nodes && props.nodes.length > 0)
      ? props.nodes.map((node) => ({ ...node }))
      : defaultNodes.map((node) => ({ ...node }));
    const links: GraphLink[] = (props.links && props.links.length > 0)
      ? props.links.map((link) => ({ ...link }))
      : defaultLinks.map((link) => ({ ...link }));

    if (nodes.length === 0) return;

    // Calculate dynamic parameters based on graph size
    const nodeCount = nodes.length;
    const linkCount = links.length;
    
    // Adjust charge based on node count (more repulsion for larger graphs)
    const chargeStrength = -Math.max(200, nodeCount * 30);
    
    // Adjust link distance based on available space and node count
    const linkDistance = Math.min(100, Math.max(50, width / (nodeCount + 1)));
    
    // Adjust collision radius based on node count
    const collisionRadius = Math.max(20, Math.min(50, width / (nodeCount * 1.5)));

    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force("link", 
        d3.forceLink<GraphNode, GraphLink>(links)
          .id(d => d.id)
          .distance(linkDistance)
          .strength(0.1)
      )
      .force("charge", d3.forceManyBody().strength(chargeStrength))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(collisionRadius))
      .alphaDecay(0.02); // Slower cooling for more stable layouts

    // Create a group for zoomable content
    const g = svg.append("g");

    const link = g.append("g")
      .attr("stroke", "var(--graph-link)")
      .attr("stroke-opacity", 0.7)
      .selectAll("line")
      .data(links)
      .join("line")
      .attr("stroke-width", 2);

    const node = g.append("g")
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
      .style("font-size", "0.8rem")
      .text(d => d.label);

    simulation.on("tick", () => {
      link
        .attr("x1", d => (d.source as any).x)
        .attr("y1", d => (d.source as any).y)
        .attr("x2", d => (d.target as any).x)
        .attr("y2", d => (d.target as any).y);

      node.attr("transform", d => `translate(${d.x},${d.y})`);
    });

    // Add zoom behavior
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .on("zoom", (event) => {
        g.attr("transform", event.transform);
      });

    svg.call(zoom);

    // Add initial transform to center the view after a brief delay to ensure SVG is properly sized
    if (width > 0 && height > 0) {
      const initialScale = Math.min(width / 960, height / 600);
      try {
        svg.call(zoom.transform, d3.zoomIdentity.translate(width / 2, height / 2).scale(initialScale));
      } catch (e) {
        // Zoom initialization may fail in test environments, silently continue
      }
    }

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
  };

  // Initial render and setup resize observer
  createEffect(() => {
    if (!svgRef) return;

    renderGraph();

    // Watch for container size changes
    const container = svgRef.parentElement;
    if (container && window.ResizeObserver) {
      resizeObserver = new ResizeObserver(() => {
        renderGraph();
      });
      resizeObserver.observe(container);
    }

    onCleanup(() => {
      if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = undefined;
      }
    });
  });

  // Re-render when props change
  createEffect(() => {
    props.nodes;
    props.links;
    props.selectedId;
    renderGraph();
  });

  return (
    <svg 
      ref={svgRef} 
      style={{ width: "100%", height: "100%", display: "block" }}
    />
  );
}
