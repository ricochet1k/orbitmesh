import { createSignal, createResource, Show, onMount, For } from "solid-js";
import { createFileRoute } from "@tanstack/solid-router";
import { apiClient } from "../api/client";
import { SigmaGraph } from "../components/graph/SigmaGraph";
import type { CodeflowQueryResult } from "../types/generated/api";
import "./dashboard.codeflow.explorer.css";

const SAMPLE_QUERIES = [
  { label: "All Nodes & Edges (Limit 100)", query: "MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100" },
  { label: "Frontend -> Backend API Links", query: "MATCH (req:APIRequest)-[r:REQUESTS_HANDLER]->(handler:APIHandler) RETURN req, r, handler" },
  { label: "Unlinked API Requests", query: "MATCH (req:APIRequest) WHERE NOT EXISTS((req)-[:REQUESTS_HANDLER]->()) RETURN req" },
  { label: "Unhandled Backend Routes", query: "MATCH (handler:APIHandler) WHERE NOT EXISTS(()-[:REQUESTS_HANDLER]->(handler)) RETURN handler" },
  { label: "High Severity Findings", query: "MATCH (f:Finding {severity: 'high'})-[r]->(n) RETURN f, r, n LIMIT 50" },
  { label: "Code Spawns in Loops", query: "MATCH (f:Function)-[r:SPAWNS]->(e:ExecutionUnit) WHERE r.inside_loop = true RETURN f, r, e" },
  { label: "API Handlers", query: "MATCH (f:File)-[r:HANDLES_ROUTE]->(a:APIHandler) RETURN f, r, a" },
  { label: "Tagged Nodes", query: "MATCH (n)-[r:HAS_TAG]->(t:Tag) RETURN n, r, t LIMIT 100" },
];

const NODE_LEGEND = [
  { label: "File",          color: "#4A5568" },
  { label: "Function",      color: "#718096" },
  { label: "ExecutionUnit", color: "#805AD5" },
  { label: "Finding",       color: "#E53E3E" },
  { label: "CallSite",      color: "#38A169" },
  { label: "APIHandler",    color: "#48BB78" },
  { label: "APIRequest",    color: "#4299E1" },
  { label: "Tag",           color: "#ED8936" },
];

function countNodesAndEdges(data: CodeflowQueryResult) {
  const nodeIds = new Set<string>();
  const edgeIds = new Set<string>();
  (data.rows ?? []).forEach((row: Record<string, unknown>) => {
    Object.values(row).forEach((val) => {
      if (!val || typeof val !== "object") return;
      const v = val as Record<string, unknown>;
      if (v.id && Array.isArray(v.labels)) {
        nodeIds.add(String(v.id));
      } else if (v.id && v.type && v.start_id !== undefined && v.end_id !== undefined) {
        edgeIds.add(String(v.id));
      }
    });
  });
  return { nodes: nodeIds.size, edges: edgeIds.size };
}

export default function CodeflowExplorer() {
  const [query, setQuery] = createSignal(SAMPLE_QUERIES[0].query);
  const [activeQuery, setActiveQuery] = createSignal("");

  const [data] = createResource(activeQuery, async (q) => {
    if (!q) return null;
    return await apiClient.queryCodeflowGraph({ query: q });
  });

  const handleRunQuery = () => setActiveQuery(query());

  onMount(() => setActiveQuery(query()));

  const handleSampleChange = (e: Event) => {
    setQuery((e.target as HTMLSelectElement).value);
  };

  const stats = () => (data() ? countNodesAndEdges(data()!) : null);
  const hasRows = () => Array.isArray(data()?.rows) && data()!.rows.length > 0;

  return (
    <div class="cfe-root">

      {/* ── Page header ──────────────────────────────────────────────── */}
      <header class="cfe-page-header">
        <div class="cfe-page-header-left">
          <div class="cfe-header-icon" aria-hidden="true">
            <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
              <circle cx="4"  cy="9"  r="2.2" fill="currentColor" opacity="0.9"/>
              <circle cx="14" cy="4"  r="2.2" fill="currentColor" opacity="0.9"/>
              <circle cx="14" cy="14" r="2.2" fill="currentColor" opacity="0.9"/>
              <line x1="6" y1="8.2"  x2="12" y2="5.2"  stroke="currentColor" stroke-width="1.3" opacity="0.6"/>
              <line x1="6" y1="9.8"  x2="12" y2="12.8" stroke="currentColor" stroke-width="1.3" opacity="0.6"/>
            </svg>
          </div>
          <div class="cfe-header-title-group">
            <h1 class="cfe-header-title">CodeFlow Explorer</h1>
            <p class="cfe-header-subtitle">Query the semantic code graph using Cypher syntax</p>
          </div>
        </div>

        <div class="cfe-header-right">
          <select
            class="cfe-sample-select"
            onChange={handleSampleChange}
            aria-label="Load a sample query"
          >
            <option value="" disabled selected>Load sample query…</option>
            <For each={SAMPLE_QUERIES}>
              {(sq) => <option value={sq.query}>{sq.label}</option>}
            </For>
          </select>
        </div>
      </header>

      {/* ── Body: editor | graph ──────────────────────────────────────── */}
      <div class="cfe-body">

        {/* Left: query editor */}
        <div class="cfe-editor-panel">

          <div class="cfe-editor-header">
            <span class="cfe-editor-label">
              <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
                <rect x="1"   y="2.8" width="10" height="1.1" rx="0.55" fill="currentColor"/>
                <rect x="1"   y="5.4" width="7"  height="1.1" rx="0.55" fill="currentColor"/>
                <rect x="1"   y="8"   width="8.5" height="1.1" rx="0.55" fill="currentColor"/>
              </svg>
              Cypher Query
            </span>
            <Show when={data.loading}>
              <span class="cfe-loading-badge">
                <span class="cfe-spinner" />
                Running…
              </span>
            </Show>
          </div>

          <textarea
            class="cfe-editor-textarea"
            value={query()}
            onInput={(e) => setQuery(e.currentTarget.value)}
            placeholder={"MATCH (n)-[r]->(m)\nRETURN n, r, m\nLIMIT 10"}
            spellcheck={false}
            autocomplete="off"
            autocorrect="off"
            aria-label="Cypher query editor"
          />

          <div class="cfe-editor-footer">
            <Show when={data.error}>
              <div class="cfe-error-banner">
                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" style="flex-shrink:0;margin-top:1px" aria-hidden="true">
                  <circle cx="6" cy="6" r="5" stroke="#f87171" stroke-width="1.2"/>
                  <line x1="6" y1="3.5" x2="6" y2="6.5" stroke="#f87171" stroke-width="1.4" stroke-linecap="round"/>
                  <circle cx="6" cy="8.2" r="0.65" fill="#f87171"/>
                </svg>
                <span class="cfe-error-text">
                  {(data.error as Error)?.message ?? "Query failed"}
                </span>
              </div>
            </Show>
            <button
              class="btn btn-primary cfe-run-btn"
              onClick={handleRunQuery}
              disabled={data.loading}
            >
              <Show when={!data.loading} fallback={<><span class="cfe-spinner" />Running…</>}>
                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
                  <path d="M2.5 2L10 6L2.5 10V2Z" fill="currentColor"/>
                </svg>
                Run Query
              </Show>
            </button>
          </div>

          {/* Node-type legend */}
          <div class="cfe-legend">
            <span class="cfe-legend-title">Node types</span>
            <div class="cfe-legend-items">
              <For each={NODE_LEGEND}>
                {(item) => (
                  <div class="cfe-legend-item">
                    <span
                      class="cfe-legend-dot"
                      style={{ background: item.color, "box-shadow": `0 0 5px ${item.color}66` }}
                    />
                    {item.label}
                  </div>
                )}
              </For>
            </div>
          </div>
        </div>

        {/* Right: graph canvas */}
        <div class="cfe-graph-panel">

          <div class="cfe-graph-toolbar">
            <div class="cfe-graph-stats">
              <Show when={stats() && !data.loading}>
                <span class="cfe-stat-pill">
                  <svg width="9" height="9" viewBox="0 0 9 9" fill="none" aria-hidden="true">
                    <circle cx="4.5" cy="4.5" r="3.5" stroke="currentColor" stroke-width="1.2"/>
                  </svg>
                  {stats()!.nodes} nodes
                </span>
                <span class="cfe-stat-pill">
                  <svg width="10" height="9" viewBox="0 0 10 9" fill="none" aria-hidden="true">
                    <line x1="1" y1="4.5" x2="9" y2="4.5" stroke="currentColor" stroke-width="1.3"/>
                    <path d="M6.5 2L9 4.5L6.5 7" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" stroke-linejoin="round"/>
                  </svg>
                  {stats()!.edges} edges
                </span>
              </Show>
            </div>
          </div>

          <div class="cfe-graph-canvas-wrap">
            <Show
              when={hasRows()}
              fallback={
                <div class="cfe-graph-empty">
                  <Show when={data.loading}>
                    <span class="cfe-spinner cfe-spinner-lg" />
                    <span class="cfe-graph-empty-text">Executing query…</span>
                  </Show>
                  <Show when={!data.loading && data() && !hasRows()}>
                    <svg class="cfe-graph-empty-icon" width="44" height="44" viewBox="0 0 44 44" fill="none" aria-hidden="true">
                      <circle cx="22" cy="22" r="20" stroke="currentColor" stroke-width="1.4"/>
                      <circle cx="10" cy="22" r="3.5" fill="currentColor"/>
                      <circle cx="34" cy="22" r="3.5" fill="currentColor"/>
                      <circle cx="22" cy="10" r="3.5" fill="currentColor"/>
                      <circle cx="22" cy="34" r="3.5" fill="currentColor"/>
                      <line x1="13.5" y1="22" x2="18.5" y2="22" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="25.5" y1="22" x2="30.5" y2="22" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="22" y1="13.5" x2="22" y2="18.5" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="22" y1="25.5" x2="22" y2="30.5" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                    </svg>
                    <span class="cfe-graph-empty-text">No results</span>
                    <span class="cfe-graph-empty-hint">Try a different query or check node labels</span>
                  </Show>
                  <Show when={!data.loading && !data()}>
                    <svg class="cfe-graph-empty-icon" width="44" height="44" viewBox="0 0 44 44" fill="none" aria-hidden="true">
                      <circle cx="22" cy="22" r="20" stroke="currentColor" stroke-width="1.4"/>
                      <circle cx="10" cy="22" r="3.5" fill="currentColor"/>
                      <circle cx="34" cy="22" r="3.5" fill="currentColor"/>
                      <circle cx="22" cy="10" r="3.5" fill="currentColor"/>
                      <circle cx="22" cy="34" r="3.5" fill="currentColor"/>
                      <line x1="13.5" y1="22" x2="18.5" y2="22" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="25.5" y1="22" x2="30.5" y2="22" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="22" y1="13.5" x2="22" y2="18.5" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                      <line x1="22" y1="25.5" x2="22" y2="30.5" stroke="currentColor" stroke-width="1.4" opacity="0.5"/>
                    </svg>
                    <span class="cfe-graph-empty-text">Write a query and press Run</span>
                    <span class="cfe-graph-empty-hint">Use the sample queries above to get started</span>
                  </Show>
                </div>
              }
            >
              <SigmaGraph data={data()!} />
            </Show>
          </div>

        </div>
      </div>

    </div>
  );
}

export const Route = createFileRoute("/dashboard/codeflow/explorer")({
  component: CodeflowExplorer,
});
