import { createEffect, onCleanup } from "solid-js"
import * as d3 from "d3"
import type { GraphLink, GraphNode, SimNode, SimLink } from "./types"

export interface AgentGraphProps {
  nodes?: GraphNode[]
  links?: GraphLink[]
  selectedId?: string
  onSelect?: (node: GraphNode) => void
}

const defaultNodes: GraphNode[] = [
  { id: "agent-1", type: "agent", label: "Agent 1", depth: 0, priority: 4 },
  { id: "task-1", type: "task", label: "Initial Setup", depth: 1, priority: 2 },
  { id: "task-2", type: "task", label: "API Layer", depth: 1, priority: 2 },
]

const defaultLinks: GraphLink[] = [
  { source: "agent-1", target: "task-1" },
  { source: "agent-1", target: "task-2" },
]

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Deterministic initial positions so the simulation starts nearly settled.
 *
 * Nodes are placed in a grid biased by depth (x) and priority (y) so the
 * force simulation only needs small adjustments rather than separating a
 * single overlapping cluster.
 */
function prePosition(
  nodes: SimNode[],
  width: number,
  height: number,
  padding: number,
): void {
  const maxDepth = Math.max(1, ...nodes.map(n => n.depth ?? 0))
  const maxPriority = Math.max(1, ...nodes.map(n => n.priority ?? 0))
  const usableW = width - padding * 2
  const usableH = height - padding * 2

  // Group by depth for staggering within a column
  const depthBuckets = new Map<number, SimNode[]>()
  for (const n of nodes) {
    const d = n.depth ?? 0
    let bucket = depthBuckets.get(d)
    if (!bucket) { bucket = []; depthBuckets.set(d, bucket) }
    bucket.push(n)
  }

  for (const n of nodes) {
    const depth = n.depth ?? 0
    const priority = n.priority ?? 0
    const bucket = depthBuckets.get(depth)!
    const idxInBucket = bucket.indexOf(n)
    const bucketSize = bucket.length

    // X: depth pushes rightward
    const xRatio = maxDepth > 0 ? depth / maxDepth : 0.5
    // Stagger within a column so nodes don't stack
    const xJitter = bucketSize > 1
      ? ((idxInBucket / (bucketSize - 1)) - 0.5) * (usableW * 0.15)
      : 0
    n.x = padding + usableW * 0.15 + xRatio * usableW * 0.7 + xJitter

    // Y: higher priority → lower Y (towards top)
    const yRatio = maxPriority > 0 ? 1 - priority / maxPriority : 0.5
    const yJitter = bucketSize > 1
      ? ((idxInBucket / (bucketSize - 1)) - 0.5) * (usableH * 0.4)
      : 0
    n.y = padding + usableH * 0.15 + yRatio * usableH * 0.7 + yJitter
  }
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function AgentGraph(props: AgentGraphProps) {
  let svgRef: SVGSVGElement | undefined
  let simulation: d3.Simulation<SimNode, SimLink> | undefined
  let resizeObserver: ResizeObserver | undefined

  const getContainerSize = () => {
    const parent = svgRef?.parentElement

    if (!parent) return { width: 400, height: 600 }
    return { width: parent.offsetWidth || 800, height: parent.offsetHeight || 500 }
  }

  // -----------------------------------------------------------------------
  // Core render – called only from the effect with pre-read reactive values
  // -----------------------------------------------------------------------
  function renderGraph(
    inputNodes: GraphNode[],
    inputLinks: GraphLink[],
    selectedId: string | undefined,
    onSelect: ((node: GraphNode) => void) | undefined,
  ) {
    if (!svgRef) return

    // Stop any prior simulation to prevent ghost ticks
    if (simulation) { simulation.stop(); simulation = undefined }

    const { width, height } = getContainerSize()
    if (width < 100 || height < 100) return

    const svg = d3.select(svgRef)
    svg.attr("width", width)
      .attr("height", height)
      .attr("viewBox", `0 0 ${width} ${height}`)
      .attr("preserveAspectRatio", "xMidYMid meet")
    svg.selectAll("*").remove()

    // Clone into SimNodes so d3 can mutate freely
    const nodes: SimNode[] = inputNodes.map(n => ({ ...n }))
    const links: SimLink[] = inputLinks.map(l => ({ ...l }))

    if (nodes.length === 0) return

    // Layout parameters
    const padding = 40
    const boundedW = Math.max(width - padding * 2, 100)
    const boundedH = Math.max(height - padding * 2, 100)
    const nodeCount = nodes.length

    const chargeStrength = -Math.max(300, (boundedW * boundedH) / (nodeCount * 20))
    const linkDistance = Math.min(150, Math.max(60, boundedW / (nodeCount * 0.8)))
    const collisionRadius = Math.max(25, Math.min(60, Math.min(boundedW, boundedH) / (nodeCount * 1.2)))

    // Pre-position nodes for fast convergence
    prePosition(nodes, width, height, padding)

    // --- Directional forces ---------------------------------------------------
    // Y: higher priority → top (lower y)
    const maxPriority = Math.max(1, ...nodes.map(n => n.priority ?? 0))
    const yTargets = nodes.map(n => {
      const p = n.priority ?? 0
      const ratio = maxPriority > 0 ? 1 - p / maxPriority : 0.5
      return padding + ratio * boundedH
    })

    // X: children should be right of parents; root nodes sit on the left
    const maxDepth = Math.max(1, ...nodes.map(n => n.depth ?? 0))
    const xTargets = nodes.map(n => {
      const d = n.depth ?? 0
      const ratio = maxDepth > 0 ? d / maxDepth : 0.5
      return padding + boundedW * 0.15 + ratio * boundedW * 0.7
    })

    // --- Simulation -----------------------------------------------------------
    simulation = d3.forceSimulation<SimNode, SimLink>(nodes)
      .force("link",
        d3.forceLink<SimNode, SimLink>(links)
          .id(d => d.id)
          .distance(linkDistance)
          .strength(0.3),
      )
      .force("charge", d3.forceManyBody<SimNode>()
        .strength(chargeStrength)
        .distanceMax(Math.max(boundedW, boundedH) * 2),
      )
      .force("x", d3.forceX<SimNode>((_, i) => xTargets[i]).strength(0.15))
      .force("y", d3.forceY<SimNode>((_, i) => yTargets[i]).strength(0.15))
      .force("collision", d3.forceCollide<SimNode>()
        .radius(collisionRadius)
        .iterations(3),
      )
      // Fast convergence: high initial alpha with aggressive decay
      .alpha(1)
      .alphaDecay(0.07)
      .alphaMin(0.001)
      .velocityDecay(0.35)

    const constrainNode = (node: SimNode) => {
      node.x = Math.max(padding, Math.min(width - padding, node.x ?? width / 2))
      node.y = Math.max(padding, Math.min(height * 3 - padding, node.y ?? height / 2))
    }

    // --- SVG elements ---------------------------------------------------------
    const g = svg.append("g")

    const linkSel = g.append("g")
      .attr("stroke", "var(--graph-link)")
      .attr("stroke-opacity", 0.7)
      .selectAll<SVGLineElement, SimLink>("line")
      .data(links)
      .join("line")
      .attr("stroke-width", 2)

    const nodeGroup = g.append("g")
      .attr("stroke", "var(--graph-stroke)")
      .attr("stroke-width", 1.2)
      .selectAll<SVGGElement, SimNode>("g")
      .data(nodes, d => d.id)
      .join(
        enter => enter.append("g")
          .call(d3.drag<SVGGElement, SimNode>()
            .on("start", dragstarted)
            .on("drag", dragged)
            .on("end", dragended))
          .on("click", (_, d) => onSelect?.(d)),
        update => update,
        exit => exit.remove(),
      )

    nodeGroup.append("circle")
      .attr("r", d => d.id === selectedId ? 18 : 14)
      .attr("fill", d => {
        if (d.type === "agent") return "var(--graph-agent)"
        if (d.type === "commit") return "var(--graph-commit)"
        return "var(--graph-task)"
      })
      .attr("stroke", d => d.id === selectedId ? "var(--graph-selected)" : "var(--graph-stroke)")
      .attr("stroke-width", d => d.id === selectedId ? 2.4 : 1.2)

    nodeGroup.append("text")
      .attr("dx", 20)
      .attr("dy", ".35em")
      .attr("stroke", "none")
      .attr("fill", "var(--graph-text)")
      .style("font-weight", d => d.id === selectedId ? "600" : "400")
      .style("font-size", "0.8rem")
      .text(d => d.label)

    // --- Tick -----------------------------------------------------------------
    simulation.on("tick", () => {
      nodes.forEach(constrainNode)

      linkSel
        .attr("x1", d => (d.source as SimNode).x ?? width / 2)
        .attr("y1", d => (d.source as SimNode).y ?? height / 2)
        .attr("x2", d => (d.target as SimNode).x ?? width / 2)
        .attr("y2", d => (d.target as SimNode).y ?? height / 2)

      nodeGroup.attr("transform", d => `translate(${d.x ?? width / 2},${d.y ?? height / 2})`)
    })

    // --- Zoom -----------------------------------------------------------------
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .on("zoom", (event) => { g.attr("transform", event.transform) })
    svg.call(zoom)

    if (width > 0 && height > 0) {
      try {
        const initialScale = Math.min(width / 960, height / 600)
        svg.call(zoom.transform,
          d3.zoomIdentity.translate(width / 2, height / 2).scale(initialScale),
        )
      } catch {
        // Zoom init may fail in test environments
      }
    }

    // --- Drag handlers --------------------------------------------------------
    function dragstarted(event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) {
      if (!event.active) simulation?.alphaTarget(0.3).restart()
      d.fx = d.x
      d.fy = d.y
    }
    function dragged(event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) {
      d.fx = event.x
      d.fy = event.y
    }
    function dragended(event: d3.D3DragEvent<SVGGElement, SimNode, SimNode>, d: SimNode) {
      if (!event.active) simulation?.alphaTarget(0)
      d.fx = null
      d.fy = null
    }
  }

  // -------------------------------------------------------------------------
  // Reactive effect – reads all props so SolidJS tracks them, then delegates
  // to the imperative D3 renderer.
  // -------------------------------------------------------------------------
  createEffect(() => {
    if (!svgRef) return

    // --- Read reactive props (this registers them as dependencies) ---
    // Fall back to defaults only when the caller omitted the prop entirely.
    // An explicit empty array means "nothing to show".
    const inputNodes = props.nodes ?? defaultNodes
    const inputLinks = props.links ?? defaultLinks
    const selectedId = props.selectedId
    const onSelect = props.onSelect

    // --- Imperative render with the snapshot values ---
    renderGraph(inputNodes, inputLinks, selectedId, onSelect)

    // Watch for container size changes
    const container = svgRef.parentElement
    if (container && typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(() => {
        renderGraph(inputNodes, inputLinks, selectedId, onSelect)
      })
      resizeObserver.observe(container)
    }

    onCleanup(() => {
      if (simulation) { simulation.stop(); simulation = undefined }
      if (resizeObserver) { resizeObserver.disconnect(); resizeObserver = undefined }
    })
  })

  return (
    <div
      class="svg-wrapper"
      style={{ width: "100%", overflow: "hidden", display: "flex" }}
    >
      <svg
        ref={svgRef}
        style={{ width: "100%", height: "100%", display: "block" }}
      />
    </div>
  )
}
