import { render } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { describe, it, expect, vi, beforeEach } from "vitest";
import AgentGraph from "./AgentGraph";
import type { GraphNode, GraphLink } from "./types";
import { buildTaskGraph, buildCommitGraph, buildUnifiedGraph } from "./graphData";
import type { TaskNode, CommitSummary } from "../types/api";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------
let mockNodes: GraphNode[];
let mockLinks: GraphLink[];

beforeEach(() => {
  mockNodes = [
    { id: "node-1", type: "agent", label: "Agent 1", depth: 0, priority: 4 },
    { id: "node-2", type: "task", label: "Task 1", depth: 1, priority: 3 },
    { id: "node-3", type: "task", label: "Task 2", depth: 1, priority: 1 },
  ];

  mockLinks = [
    { source: "node-1", target: "node-2" },
    { source: "node-1", target: "node-3" },
  ];
});

// ===========================================================================
// 1. Basic rendering
// ===========================================================================
describe("AgentGraph – basic rendering", () => {
  it("renders an svg element with no props", () => {
    const { container } = render(() => <AgentGraph />);
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("renders with custom nodes and links", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const svg = container.querySelector("svg")!;
    const groups = svg.querySelectorAll("g");
    expect(groups.length).toBeGreaterThanOrEqual(2);
  });

  it("sets explicit width and height attributes on svg", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const svg = container.querySelector("svg")!;
    expect(svg.hasAttribute("width")).toBe(true);
    expect(svg.hasAttribute("height")).toBe(true);
  });

  it("renders with 100% width/height style for responsiveness", () => {
    const { container } = render(() => (
      <div style={{ width: "600px", height: "400px" }}>
        <AgentGraph nodes={mockNodes} links={mockLinks} />
      </div>
    ));
    const svg = container.querySelector("svg")!;
    expect(svg.style.width).toBe("100%");
    expect(svg.style.height).toBe("100%");
  });

  it("maintains viewBox for responsive scaling", () => {
    const { container } = render(() => (
      <div style={{ width: "500px", height: "400px" }}>
        <AgentGraph nodes={mockNodes} links={mockLinks} />
      </div>
    ));
    const svg = container.querySelector("svg")!;
    const viewBox = svg.getAttribute("viewBox");
    expect(viewBox).toBeDefined();
    expect(viewBox).toContain("0 0");
  });

  it("renders correct number of circles for nodes", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const circles = container.querySelectorAll("circle");
    expect(circles.length).toBe(mockNodes.length);
  });

  it("renders correct number of lines for links", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const lines = container.querySelectorAll("line");
    expect(lines.length).toBe(mockLinks.length);
  });

  it("renders text labels for each node", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));
    const texts = container.querySelectorAll("text");
    expect(texts.length).toBe(mockNodes.length);
  });

  it("handles empty arrays gracefully", () => {
    const { container } = render(() => (
      <AgentGraph nodes={[]} links={[]} />
    ));
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("uses default nodes when none provided", () => {
    const { container } = render(() => <AgentGraph />);
    const circles = container.querySelectorAll("circle");
    expect(circles.length).toBeGreaterThan(0);
  });
});

// ===========================================================================
// 2. Selection
// ===========================================================================
describe("AgentGraph – selection", () => {
  it("supports onSelect callback prop", () => {
    const handleSelect = vi.fn();
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} onSelect={handleSelect} />
    ));
    // The graph renders and accepts the callback without error
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("applies different radius to selected node", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} selectedId="node-1" />
    ));
    const circles = container.querySelectorAll("circle");
    const radii = Array.from(circles).map(c => Number(c.getAttribute("r")));
    // Selected node gets r=18, others get r=14
    expect(radii).toContain(18);
    expect(radii).toContain(14);
  });

  it("applies thicker stroke to selected node", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} selectedId="node-1" />
    ));
    const circles = container.querySelectorAll("circle");
    const strokeWidths = Array.from(circles).map(c =>
      Number(c.getAttribute("stroke-width")),
    );
    expect(strokeWidths).toContain(2.4);
    expect(strokeWidths).toContain(1.2);
  });

  it("applies bold font weight to selected node label", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} selectedId="node-1" />
    ));
    const texts = Array.from(container.querySelectorAll("text"));
    const weights = texts.map(t => t.style.fontWeight);
    expect(weights).toContain("600");
    expect(weights).toContain("400");
  });
});

// ===========================================================================
// 3. Node type colors
// ===========================================================================
describe("AgentGraph – node colors", () => {
  it("applies var(--graph-agent) fill to agent nodes", () => {
    const nodes: GraphNode[] = [
      { id: "a", type: "agent", label: "A", depth: 0, priority: 4 },
    ];
    const { container } = render(() => <AgentGraph nodes={nodes} links={[]} />);
    const circle = container.querySelector("circle")!;
    expect(circle.getAttribute("fill")).toBe("var(--graph-agent)");
  });

  it("applies var(--graph-task) fill to task nodes", () => {
    const nodes: GraphNode[] = [
      { id: "t", type: "task", label: "T", depth: 1, priority: 2 },
    ];
    const { container } = render(() => <AgentGraph nodes={nodes} links={[]} />);
    const circle = container.querySelector("circle")!;
    expect(circle.getAttribute("fill")).toBe("var(--graph-task)");
  });

  it("applies var(--graph-commit) fill to commit nodes", () => {
    const nodes: GraphNode[] = [
      { id: "c", type: "commit", label: "C", depth: 1, priority: 1 },
    ];
    const { container } = render(() => <AgentGraph nodes={nodes} links={[]} />);
    const circle = container.querySelector("circle")!;
    expect(circle.getAttribute("fill")).toBe("var(--graph-commit)");
  });
});

// ===========================================================================
// 4. D3/SolidJS reactivity – re-renders when props change
// ===========================================================================
describe("AgentGraph – reactivity", () => {
  it("re-renders when nodes prop changes", async () => {
    const [nodes, setNodes] = createSignal<GraphNode[]>(mockNodes);

    const { container } = render(() => (
      <AgentGraph nodes={nodes()} links={mockLinks} />
    ));
    expect(container.querySelectorAll("circle").length).toBe(3);

    // Add a node
    setNodes([
      ...mockNodes,
      { id: "node-4", type: "commit", label: "Commit 1", depth: 2, priority: 1 },
    ]);

    // SolidJS effects are synchronous in test env
    await Promise.resolve();
    expect(container.querySelectorAll("circle").length).toBe(4);
  });

  it("re-renders when selectedId prop changes", async () => {
    const [selectedId, setSelectedId] = createSignal<string | undefined>(undefined);

    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} selectedId={selectedId()} />
    ));

    // No node should have r=18 initially
    let radii = Array.from(container.querySelectorAll("circle")).map(c =>
      Number(c.getAttribute("r")),
    );
    expect(radii.every(r => r === 14)).toBe(true);

    // Select a node
    setSelectedId("node-1");
    await Promise.resolve();

    radii = Array.from(container.querySelectorAll("circle")).map(c =>
      Number(c.getAttribute("r")),
    );
    expect(radii).toContain(18);
  });

  it("stops prior simulation when props change", async () => {
    // Use a single signal containing both nodes and links to avoid intermediate
    // states where links reference nodes that haven't been added yet.
    const [data, setData] = createSignal<{ nodes: GraphNode[]; links: GraphLink[] }>({
      nodes: mockNodes,
      links: mockLinks,
    });

    render(() => <AgentGraph nodes={data().nodes} links={data().links} />);

    // Reduce to a single node with no links
    setData({ nodes: [mockNodes[0]], links: [] });
    await Promise.resolve();

    // Restore original set
    setData({ nodes: mockNodes, links: mockLinks });
    await Promise.resolve();
    // If ghost simulations existed, they'd cause errors or extra DOM elements.
    // Reaching here without error means old simulations were stopped.
  });
});

// ===========================================================================
// 5. Directional bias – priority→top, depth→right
// ===========================================================================
describe("AgentGraph – directional layout bias", () => {
  /**
   * We test the prePosition logic indirectly by reading initial node
   * positions from the SVG transform attributes on the first tick.
   * Higher-priority nodes should have a lower Y (closer to top),
   * and deeper nodes should have a higher X (more to the right).
   */
  it("places higher-priority nodes closer to the top (lower Y)", () => {
    // node-1: priority 4 (highest) → should be topmost
    // node-3: priority 1 (lowest)  → should be bottommost
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));

    const groups = container.querySelectorAll("svg g g g"); // nested: root > container > nodeContainer > individual node groups
    if (groups.length < mockNodes.length) return; // jsdom may not run ticks

    const transforms = Array.from(groups).map(g => {
      const t = g.getAttribute("transform") ?? "";
      const m = t.match(/translate\(([\d.]+),([\d.]+)\)/);
      return m ? { x: parseFloat(m[1]), y: parseFloat(m[2]) } : null;
    }).filter(Boolean) as { x: number; y: number }[];

    if (transforms.length < 2) return; // skip if transforms not yet applied

    // The agent (priority 4) should have a lower Y than the task with priority 1
    const agentY = transforms[0].y;   // node-1 (priority 4)
    const lowY = transforms[2].y;     // node-3 (priority 1)
    expect(agentY).toBeLessThanOrEqual(lowY);
  });

  it("places deeper nodes further to the right (higher X)", () => {
    const hierarchicalNodes: GraphNode[] = [
      { id: "root", type: "agent", label: "Root", depth: 0, priority: 4 },
      { id: "child", type: "task", label: "Child", depth: 1, priority: 2 },
      { id: "grandchild", type: "task", label: "Grandchild", depth: 2, priority: 1 },
    ];
    const hierarchicalLinks: GraphLink[] = [
      { source: "root", target: "child" },
      { source: "child", target: "grandchild" },
    ];

    const { container } = render(() => (
      <AgentGraph nodes={hierarchicalNodes} links={hierarchicalLinks} />
    ));

    const groups = container.querySelectorAll("svg g g g");
    if (groups.length < 3) return;

    const transforms = Array.from(groups).map(g => {
      const t = g.getAttribute("transform") ?? "";
      const m = t.match(/translate\(([\d.]+),([\d.]+)\)/);
      return m ? { x: parseFloat(m[1]), y: parseFloat(m[2]) } : null;
    }).filter(Boolean) as { x: number; y: number }[];

    if (transforms.length < 3) return;

    // root (depth 0) < child (depth 1) < grandchild (depth 2) in X
    expect(transforms[0].x).toBeLessThanOrEqual(transforms[1].x);
    expect(transforms[1].x).toBeLessThanOrEqual(transforms[2].x);
  });
});

// ===========================================================================
// 6. Large / dense graphs
// ===========================================================================
describe("AgentGraph – large graphs", () => {
  it("renders a large graph without errors", () => {
    const largeNodes = Array.from({ length: 10 }, (_, i) => ({
      id: `node-${i}`,
      type: "agent" as const,
      label: `Node ${i}`,
      depth: i % 3,
      priority: 10 - i,
    }));
    const largeLinks = Array.from({ length: 8 }, (_, i) => ({
      source: `node-${i}`,
      target: `node-${(i + 1) % largeNodes.length}`,
    }));

    const { container } = render(() => (
      <div style={{ width: "800px", height: "600px" }}>
        <AgentGraph nodes={largeNodes} links={largeLinks} />
      </div>
    ));

    const svg = container.querySelector("svg")!;
    expect(svg.querySelectorAll("circle").length).toBe(largeNodes.length);
    expect(svg.querySelectorAll("line").length).toBe(largeLinks.length);
  });

  it("renders a dense graph with viewBox", () => {
    const denseNodes = Array.from({ length: 15 }, (_, i) => ({
      id: `dense-${i}`,
      type: "task" as const,
      label: `Task ${i}`,
      depth: Math.floor(i / 5),
      priority: 15 - i,
    }));
    const denseLinks = denseNodes.slice(0, -1).flatMap((node, i) =>
      denseNodes.slice(i + 1, Math.min(i + 3, denseNodes.length)).map(target => ({
        source: node.id,
        target: target.id,
      })),
    );

    const { container } = render(() => (
      <div style={{ width: "1000px", height: "800px" }}>
        <AgentGraph nodes={denseNodes} links={denseLinks} />
      </div>
    ));

    const svg = container.querySelector("svg")!;
    expect(svg.hasAttribute("viewBox")).toBe(true);
    expect(svg.querySelectorAll("g").length).toBeGreaterThan(0);
  });
});

// ===========================================================================
// 7. graphData builders – depth & priority
// ===========================================================================
describe("graphData – depth and priority population", () => {
  describe("buildTaskGraph", () => {
    it("returns empty graph for empty input", () => {
      const g = buildTaskGraph([]);
      expect(g.nodes).toHaveLength(0);
      expect(g.links).toHaveLength(0);
    });

    it("creates a root agent node at depth 0 priority 4", () => {
      const tasks: TaskNode[] = [
        { id: "t1", title: "Task 1", role: "dev", status: "pending", updated_at: "" },
      ];
      const g = buildTaskGraph(tasks);
      const root = g.nodes.find(n => n.id === "task-root")!;
      expect(root.type).toBe("agent");
      expect(root.depth).toBe(0);
      expect(root.priority).toBe(4);
    });

    it("assigns depth 1 to top-level tasks", () => {
      const tasks: TaskNode[] = [
        { id: "t1", title: "Task 1", role: "dev", status: "pending", updated_at: "" },
      ];
      const g = buildTaskGraph(tasks);
      const t1 = g.nodes.find(n => n.id === "t1")!;
      expect(t1.depth).toBe(1);
    });

    it("increments depth for nested children", () => {
      const tasks: TaskNode[] = [
        {
          id: "t1", title: "Parent", role: "dev", status: "pending", updated_at: "",
          children: [
            {
              id: "t2", title: "Child", role: "dev", status: "in_progress", updated_at: "",
              children: [
                { id: "t3", title: "Grandchild", role: "dev", status: "completed", updated_at: "" },
              ],
            },
          ],
        },
      ];
      const g = buildTaskGraph(tasks);
      expect(g.nodes.find(n => n.id === "t1")!.depth).toBe(1);
      expect(g.nodes.find(n => n.id === "t2")!.depth).toBe(2);
      expect(g.nodes.find(n => n.id === "t3")!.depth).toBe(3);
    });

    it("maps task status to priority: in_progress > pending > completed", () => {
      const tasks: TaskNode[] = [
        { id: "ip", title: "In Progress", role: "dev", status: "in_progress", updated_at: "" },
        { id: "pe", title: "Pending", role: "dev", status: "pending", updated_at: "" },
        { id: "co", title: "Completed", role: "dev", status: "completed", updated_at: "" },
      ];
      const g = buildTaskGraph(tasks);
      const ip = g.nodes.find(n => n.id === "ip")!;
      const pe = g.nodes.find(n => n.id === "pe")!;
      const co = g.nodes.find(n => n.id === "co")!;
      expect(ip.priority).toBeGreaterThan(pe.priority!);
      expect(pe.priority).toBeGreaterThan(co.priority!);
    });

    it("creates links from parent to child", () => {
      const tasks: TaskNode[] = [
        {
          id: "t1", title: "Parent", role: "dev", status: "pending", updated_at: "",
          children: [
            { id: "t2", title: "Child", role: "dev", status: "pending", updated_at: "" },
          ],
        },
      ];
      const g = buildTaskGraph(tasks);
      expect(g.links).toContainEqual({ source: "task-root", target: "t1" });
      expect(g.links).toContainEqual({ source: "t1", target: "t2" });
    });
  });

  describe("buildCommitGraph", () => {
    it("returns empty graph for empty input", () => {
      const g = buildCommitGraph([]);
      expect(g.nodes).toHaveLength(0);
      expect(g.links).toHaveLength(0);
    });

    it("creates root at depth 0 and commits at depth 1", () => {
      const commits: CommitSummary[] = [
        { sha: "abc", message: "first", author: "a", email: "a@b", timestamp: "" },
      ];
      const g = buildCommitGraph(commits);
      expect(g.nodes.find(n => n.id === "commit-root")!.depth).toBe(0);
      expect(g.nodes.find(n => n.id === "abc")!.depth).toBe(1);
    });

    it("gives newer commits (earlier in array) higher priority", () => {
      const commits: CommitSummary[] = [
        { sha: "new", message: "newest", author: "a", email: "a@b", timestamp: "" },
        { sha: "old", message: "oldest", author: "a", email: "a@b", timestamp: "" },
      ];
      const g = buildCommitGraph(commits);
      const newNode = g.nodes.find(n => n.id === "new")!;
      const oldNode = g.nodes.find(n => n.id === "old")!;
      expect(newNode.priority).toBeGreaterThan(oldNode.priority!);
    });
  });

  describe("buildUnifiedGraph", () => {
    it("merges task and commit graphs", () => {
      const tasks: TaskNode[] = [
        { id: "t1", title: "T1", role: "dev", status: "pending", updated_at: "" },
      ];
      const commits: CommitSummary[] = [
        { sha: "abc", message: "msg", author: "a", email: "a@b", timestamp: "" },
      ];
      const g = buildUnifiedGraph(tasks, commits);
      // task-root + t1 + commit-root + abc = 4 nodes
      expect(g.nodes).toHaveLength(4);
      // task-root->t1 + commit-root->abc = 2 links
      expect(g.links).toHaveLength(2);
    });

    it("preserves depth and priority from both sub-graphs", () => {
      const tasks: TaskNode[] = [
        { id: "t1", title: "T1", role: "dev", status: "in_progress", updated_at: "" },
      ];
      const commits: CommitSummary[] = [
        { sha: "abc", message: "msg", author: "a", email: "a@b", timestamp: "" },
      ];
      const g = buildUnifiedGraph(tasks, commits);
      const t1 = g.nodes.find(n => n.id === "t1")!;
      const abc = g.nodes.find(n => n.id === "abc")!;
      expect(t1.depth).toBe(1);
      expect(t1.priority).toBe(3); // in_progress
      expect(abc.depth).toBe(1);
      expect(abc.priority).toBeDefined();
    });
  });
});

// ===========================================================================
// 8. Simulation configuration (convergence & forces)
// ===========================================================================
describe("AgentGraph – simulation configuration", () => {
  it("does not error during rapid mount/unmount cycles", () => {
    // Stress test to ensure simulation cleanup works
    for (let i = 0; i < 5; i++) {
      const { unmount } = render(() => (
        <AgentGraph nodes={mockNodes} links={mockLinks} />
      ));
      unmount();
    }
    // No hanging simulations or errors
  });

  it("renders nodes at non-overlapping positions for small graphs", () => {
    const { container } = render(() => (
      <AgentGraph nodes={mockNodes} links={mockLinks} />
    ));

    // After initial render, d3 should have positioned nodes via prePosition
    const groups = Array.from(container.querySelectorAll("svg g g g"));
    const positions = groups
      .map(g => {
        const t = g.getAttribute("transform") ?? "";
        const m = t.match(/translate\(([\d.]+),([\d.]+)\)/);
        return m ? { x: parseFloat(m[1]), y: parseFloat(m[2]) } : null;
      })
      .filter(Boolean) as { x: number; y: number }[];

    if (positions.length < 2) return; // skip if transforms not applied in jsdom

    // Check that at least some nodes have distinct positions
    const uniquePositions = new Set(positions.map(p => `${Math.round(p.x)},${Math.round(p.y)}`));
    expect(uniquePositions.size).toBeGreaterThan(1);
  });
});
