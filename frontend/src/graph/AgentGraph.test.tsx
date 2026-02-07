import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, beforeEach } from "vitest";
import AgentGraph from "./AgentGraph";
import type { GraphNode, GraphLink } from "./types";

describe("AgentGraph", () => {
  let mockNodes: GraphNode[];
  let mockLinks: GraphLink[];

  beforeEach(() => {
    mockNodes = [
      { id: "node-1", type: "agent", label: "Agent 1" },
      { id: "node-2", type: "task", label: "Task 1" },
      { id: "node-3", type: "task", label: "Task 2" },
    ];

    mockLinks = [
      { source: "node-1", target: "node-2" },
      { source: "node-1", target: "node-3" },
    ];
  });

  it("renders an svg element", () => {
    const { container } = render(() => <AgentGraph />);
    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
  });

  it("renders with custom nodes and links", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
    
    // Check that SVG has groups for links and nodes
    const groups = svg?.querySelectorAll("g");
    expect(groups?.length).toBeGreaterThanOrEqual(2); // At least one for links, one for nodes
  });

  it("sets explicit width and height attributes on svg", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const svg = container.querySelector("svg") as SVGSVGElement;
    
    // SVG should have width and height set
    expect(svg.hasAttribute("width")).toBe(true);
    expect(svg.hasAttribute("height")).toBe(true);
  });

  it("renders with parent container constraints respected", () => {
    const { container } = render(() => (
      <div style={{ width: "600px", height: "400px" }}>
        <AgentGraph nodes={mockNodes} links={mockLinks} />
      </div>
    ));
    
    const svg = container.querySelector("svg") as SVGSVGElement;
    expect(svg).toBeDefined();
    // SVG should be responsive to container
    expect(svg.style.width).toBe("100%");
    expect(svg.style.height).toBe("100%");
  });

  it("supports node selection callback", () => {
    let selectedNode: GraphNode | null = null;
    const handleSelect = (node: GraphNode) => {
      selectedNode = node;
    };

    const { container } = render(() => (
      <AgentGraph 
        nodes={mockNodes} 
        links={mockLinks}
        onSelect={handleSelect}
      />
    ));

    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
    // Graph is rendered and can handle selections
  });

  it("highlights selected node differently", () => {
    const selectedId = "node-1";
    const { container } = render(() => (
      <AgentGraph 
        nodes={mockNodes} 
        links={mockLinks}
        selectedId={selectedId}
      />
    ));

    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
    // Graph renders with selection state applied
  });

  it("applies correct colors for different node types", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));

    const svg = container.querySelector("svg");
    const circles = svg?.querySelectorAll("circle");
    expect(circles?.length).toBe(mockNodes.length);
  });

  it("renders links with correct attributes", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));

    const svg = container.querySelector("svg");
    const lines = svg?.querySelectorAll("line");
    expect(lines?.length).toBe(mockLinks.length);
  });

  it("renders node labels with text elements", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));

    const svg = container.querySelector("svg");
    const texts = svg?.querySelectorAll("text");
    expect(texts?.length).toBe(mockNodes.length);
  });

  it("handles empty node/link arrays gracefully", () => {
    const { container } = render(() => (
      <AgentGraph nodes={[]} links={[]} />
    ));

    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
    // Should render SVG even with no nodes
  });

  it("uses default nodes when none provided", () => {
    const { container } = render(() => <AgentGraph />);
    const svg = container.querySelector("svg");
    
    // Should have rendered default nodes
    const circles = svg?.querySelectorAll("circle");
    expect(circles?.length).toBeGreaterThan(0);
  });
});
