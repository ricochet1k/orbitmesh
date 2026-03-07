import { createSignal, createResource, Show, onMount, onCleanup, createEffect } from "solid-js";
import { apiClient } from "../api/client";
import { SigmaGraph } from "../components/graph/SigmaGraph";

// Sample queries based on MVP
const SAMPLE_QUERIES = [
  { label: "All Nodes & Edges (Limit 100)", query: "MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100" },
  { label: "High Severity Findings", query: "MATCH (f:Finding {severity: 'high'})-[r]->(n) RETURN f, r, n LIMIT 50" },
  { label: "Code Spawns in Loops", query: "MATCH (f:Function)-[r:SPAWNS]->(e:ExecutionUnit) WHERE r.inside_loop = true RETURN f, r, e" },
  { label: "API Handlers", query: "MATCH (f:File)-[r:HANDLES_ROUTE]->(a:APIHandler) RETURN f, r, a" }
];

export default function CodeflowExplorer() {
  const [query, setQuery] = createSignal(SAMPLE_QUERIES[0].query);
  const [activeQuery, setActiveQuery] = createSignal("");

  const [data, { refetch }] = createResource(activeQuery, async (q) => {
    if (!q) return null;
    return await apiClient.queryCodeflowGraph({ query: q });
  });

  const handleRunQuery = () => {
    setActiveQuery(query());
  };

  // Run on mount
  onMount(() => {
    setActiveQuery(query());
  });

  const handleSampleChange = (e: Event) => {
    const target = e.target as HTMLSelectElement;
    setQuery(target.value);
  };

  return (
    <div class="flex flex-col h-full bg-slate-900 text-slate-200">
      <div class="p-4 border-b border-slate-700 bg-slate-800 flex flex-col gap-4 shadow-sm z-10">
        <div class="flex items-center justify-between">
          <h1 class="text-xl font-semibold">CodeFlow Explorer</h1>
          <select
            class="bg-slate-700 border border-slate-600 text-sm rounded px-3 py-1.5 focus:outline-none focus:border-blue-500"
            onChange={handleSampleChange}
          >
            <option value="" disabled selected>Sample Queries...</option>
            {SAMPLE_QUERIES.map(sq => (
              <option value={sq.query}>{sq.label}</option>
            ))}
          </select>
        </div>

        <div class="flex gap-4">
          <textarea
            class="flex-1 bg-slate-900 border border-slate-600 rounded p-2 font-mono text-sm min-h-[80px] focus:outline-none focus:border-blue-500"
            value={query()}
            onInput={(e) => setQuery(e.currentTarget.value)}
            placeholder="MATCH (n) RETURN n LIMIT 10"
          />
          <button
            class="bg-blue-600 hover:bg-blue-500 text-white px-6 py-2 rounded font-medium transition-colors whitespace-nowrap self-start"
            onClick={handleRunQuery}
            disabled={data.loading}
          >
            {data.loading ? "Running..." : "Run Query"}
          </button>
        </div>

        {data.error && (
          <div class="bg-red-900/50 border border-red-700 text-red-200 px-4 py-2 rounded text-sm">
            Error: {data.error.message || "Failed to execute query"}
          </div>
        )}
      </div>

      <div class="flex-1 relative overflow-hidden bg-slate-950">
        <Show when={data() && Array.isArray(data()!.rows) && data()!.rows.length > 0} fallback={
          <div class="absolute inset-0 flex items-center justify-center text-slate-500">
            {data.loading ? "Loading graph..." : (activeQuery() ? "No results found" : "Enter a query to visualize the graph")}
          </div>
        }>
          <SigmaGraph data={data()!} />
        </Show>
      </div>
    </div>
  );
}

import { createFileRoute } from "@tanstack/solid-router";

export const Route = createFileRoute("/dashboard/codeflow/explorer")({
  component: CodeflowExplorer,
});
