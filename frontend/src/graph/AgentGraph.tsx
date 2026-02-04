import { onMount, onCleanup } from "solid-js";
import * as d3 from "d3";

interface Node extends d3.SimulationNodeDatum {
  id: string;
  type: "agent" | "task" | "commit";
  label: string;
}

interface Link extends d3.SimulationLinkDatum<Node> {
  source: string;
  target: string;
}

export default function AgentGraph() {
  let svgRef: SVGSVGElement | undefined;

  onMount(() => {
    if (!svgRef) return;

    const width = svgRef.clientWidth;
    const height = svgRef.clientHeight;

    const svg = d3.select(svgRef);
    
    // Initial data
    const nodes: Node[] = [
      { id: "agent-1", type: "agent", label: "Agent 1" },
      { id: "task-1", type: "task", label: "Initial Setup" },
      { id: "task-2", type: "task", label: "API Layer" },
    ];

    const links: Link[] = [
      { source: "agent-1", target: "task-2" },
    ];

    const simulation = d3.forceSimulation<Node>(nodes)
      .force("link", d3.forceLink<Node, Link>(links).id(d).id)
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collision", d3.forceCollide().radius(40));

    const link = svg.append("g")
      .attr("stroke", "#999")
      .attr("stroke-opacity", 0.6)
      .selectAll("line")
      .data(links)
      .join("line")
      .attr("stroke-width", 2);

    const node = svg.append("g")
      .attr("stroke", "#fff")
      .attr("stroke-width", 1.5)
      .selectAll("g")
      .data(nodes)
      .join("g")
      .call(d3.drag<any, Node>()
        .on("start", dragstarted)
        .on("drag", dragged)
        .on("end", dragended));

    node.append("circle")
      .attr("r", 15)
      .attr("fill", d => d.type === "agent" ? "#ff4444" : "#4444ff");

    node.append("text")
      .attr("dx", 20)
      .attr("dy", ".35em")
      .attr("stroke", "none")
      .attr("fill", "#333")
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
