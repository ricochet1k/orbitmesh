import { createEffect, createMemo, createResource, createSignal, For, Show, onCleanup } from "solid-js";
import { apiClient } from "../api/client";
import type { TaskNode, TaskStatus } from "../types/api";
import AgentGraph from "../graph/AgentGraph";
import { buildTaskGraph } from "../graph/graphData";

interface ContextMenuState {
  id: string;
  x: number;
  y: number;
}

const statusLabels: Record<TaskStatus, string> = {
  pending: "Pending",
  in_progress: "In Progress",
  completed: "Completed",
};

export default function TaskTreeView() {
  const [treeResponse] = createResource(apiClient.getTaskTree);
  const [treeData, setTreeData] = createSignal<TaskNode[]>([]);
  const [search, setSearch] = createSignal("");
  const [roleFilter, setRoleFilter] = createSignal("all");
  const [statusFilter, setStatusFilter] = createSignal("all");
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set());
  const [menu, setMenu] = createSignal<ContextMenuState | null>(null);
  const [selectedId, setSelectedId] = createSignal(getInitialTaskId());

  const nodeRefs = new Map<string, HTMLDivElement>();

  createEffect(() => {
    if (treeResponse()) {
      setTreeData(treeResponse()?.tasks ?? []);
    }
  });

  createEffect(() => {
    if (treeData().length === 0 || expanded().size > 0) return;
    const next = new Set<string>();
    treeData().forEach((task) => next.add(task.id));
    setExpanded(next);
  });

  createEffect(() => {
    const id = selectedId();
    if (!id) return;
    const path = findPath(treeData(), id);
    if (path.length > 0) {
      const next = new Set(expanded());
      path.forEach((nodeId) => next.add(nodeId));
      setExpanded(next);
    }
    const node = nodeRefs.get(id);
    if (node) {
      requestAnimationFrame(() => {
        node.scrollIntoView({ block: "center", behavior: "smooth" });
      });
    }
  });

  createEffect(() => {
    const handleClick = () => setMenu(null);
    window.addEventListener("click", handleClick);
    onCleanup(() => window.removeEventListener("click", handleClick));
  });

  const roles = createMemo(() => {
    const unique = new Set<string>();
    const collect = (nodes: TaskNode[]) => {
      nodes.forEach((node) => {
        unique.add(node.role);
        collect(node.children ?? []);
      });
    };
    collect(treeData());
    return Array.from(unique.values()).sort();
  });

  const filteredTree = createMemo(() => {
    const query = search().trim().toLowerCase();
    const role = roleFilter();
    const status = statusFilter();

    const matches = (node: TaskNode) => {
      const roleMatches = role === "all" || node.role === role;
      const statusMatches = status === "all" || node.status === status;
      const queryMatches = query === "" || node.title.toLowerCase().includes(query);
      return roleMatches && statusMatches && queryMatches;
    };

    const filterNodes = (nodes: TaskNode[]): TaskNode[] => {
      return nodes.flatMap((node) => {
        const children = filterNodes(node.children ?? []);
        if (matches(node) || children.length > 0) {
          return [{ ...node, children }];
        }
        return [];
      });
    };

    return filterNodes(treeData());
  });

  const graphData = createMemo(() => buildTaskGraph(treeData()));

  const toggleExpanded = (id: string) => {
    const next = new Set(expanded());
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    setExpanded(next);
  };

  const selectTask = (id: string) => {
    setSelectedId(id);
    updateTaskQuery(id);
  };

  const onContextMenu = (event: MouseEvent, id: string) => {
    event.preventDefault();
    setMenu({ id, x: event.clientX, y: event.clientY });
  };

  const updateStatus = (id: string, status: TaskStatus) => {
    setTreeData((prev) => updateNode(prev, id, (node) => ({
      ...node,
      status,
      updated_at: new Date().toISOString(),
    })));
    setMenu(null);
  };

  const addSubtask = (id: string) => {
    const title = window.prompt("Subtask title");
    if (!title) return;
    const newNode: TaskNode = {
      id: `task-${Date.now().toString(36)}`,
      title,
      role: "developer",
      status: "pending",
      updated_at: new Date().toISOString(),
      children: [],
    };
    setTreeData((prev) => updateNode(prev, id, (node) => ({
      ...node,
      children: [...(node.children ?? []), newNode],
      updated_at: new Date().toISOString(),
    })));
    const next = new Set(expanded());
    next.add(id);
    setExpanded(next);
    setMenu(null);
  };

  return (
    <div class="task-tree-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Task operations</p>
          <h1>Task Tree</h1>
          <p class="dashboard-subtitle">
            Explore hierarchical task progress, filter by role or status, and sync selections with the system graph.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Tasks tracked</p>
            <strong>{countTasks(treeData())}</strong>
          </div>
          <div class="meta-card">
            <p>In progress</p>
            <strong>{countTasks(treeData(), "in_progress")}</strong>
          </div>
          <div class="meta-card">
            <p>Completed</p>
            <strong>{countTasks(treeData(), "completed")}</strong>
          </div>
        </div>
      </header>

      <main class="task-tree-layout">
        <section class="task-tree-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Hierarchy</p>
              <h2>Task Tree Viewer</h2>
            </div>
            <span class="panel-pill">Live</span>
          </div>

          <div class="task-tree-controls">
            <input
              type="search"
              placeholder="Search tasks"
              value={search()}
              onInput={(event) => setSearch(event.currentTarget.value)}
            />
            <select value={roleFilter()} onChange={(event) => setRoleFilter(event.currentTarget.value)}>
              <option value="all">All roles</option>
              <For each={roles()}>{(role) => <option value={role}>{role}</option>}</For>
            </select>
            <select value={statusFilter()} onChange={(event) => setStatusFilter(event.currentTarget.value)}>
              <option value="all">All status</option>
              <option value="pending">Pending</option>
              <option value="in_progress">In Progress</option>
              <option value="completed">Completed</option>
            </select>
          </div>

          <Show when={!treeResponse.loading} fallback={<p class="muted">Loading task tree...</p>}>
            <div class="task-tree">
              <For each={filteredTree()}>
                {(node) => (
                  <TaskNodeRow
                    node={node}
                    depth={0}
                    expanded={expanded}
                    selectedId={selectedId}
                    onToggle={toggleExpanded}
                    onSelect={selectTask}
                    onContextMenu={onContextMenu}
                    registerRef={nodeRefs}
                  />
                )}
              </For>
              <Show when={filteredTree().length === 0}>
                <p class="muted">No tasks match the active filters.</p>
              </Show>
            </div>
          </Show>
        </section>

        <section class="graph-view compact">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Task topology</p>
              <h2>Graph Sync</h2>
            </div>
            <span class="panel-pill neutral">Linked</span>
          </div>
          <div id="graph-container">
            <AgentGraph
              nodes={graphData().nodes}
              links={graphData().links}
              selectedId={selectedId()}
              onSelect={(node) => node.type === "task" && selectTask(node.id)}
            />
          </div>
        </section>
      </main>

      <Show when={menu()}>
        {(state) => (
          <div
            class="context-menu"
            style={{ top: `${state().y}px`, left: `${state().x}px` }}
          >
            <button type="button" onClick={() => addSubtask(state().id)}>Add subtask</button>
            <button type="button" onClick={() => updateStatus(state().id, "in_progress")}>Mark in progress</button>
            <button type="button" onClick={() => updateStatus(state().id, "completed")}>Mark completed</button>
            <button type="button" onClick={() => updateStatus(state().id, "pending")}>Mark pending</button>
          </div>
        )}
      </Show>
    </div>
  );
}

interface TaskNodeRowProps {
  node: TaskNode;
  depth: number;
  expanded: () => Set<string>;
  selectedId: () => string;
  onToggle: (id: string) => void;
  onSelect: (id: string) => void;
  onContextMenu: (event: MouseEvent, id: string) => void;
  registerRef: Map<string, HTMLDivElement>;
}

function TaskNodeRow(props: TaskNodeRowProps) {
  const isExpanded = () => props.expanded().has(props.node.id);
  const hasChildren = () => (props.node.children ?? []).length > 0;

  return (
    <div class="task-node-block">
      <div
        class={`task-node ${props.selectedId() === props.node.id ? "selected" : ""}`}
        style={{ "padding-left": `${props.depth * 18 + 8}px` }}
        ref={(el) => props.registerRef.set(props.node.id, el)}
        onClick={() => props.onSelect(props.node.id)}
        onContextMenu={(event) => props.onContextMenu(event, props.node.id)}
      >
        <button
          type="button"
          class="expand-toggle"
          disabled={!hasChildren()}
          aria-expanded={isExpanded()}
          onClick={(event) => {
            event.stopPropagation();
            if (hasChildren()) props.onToggle(props.node.id);
          }}
        >
          {hasChildren() ? (isExpanded() ? "-" : "+") : ""}
        </button>
        <div class="task-node-info">
          <p>{props.node.title}</p>
          <span class="task-meta">{props.node.role}</span>
        </div>
        <span class={`task-status ${props.node.status}`}>{statusLabels[props.node.status]}</span>
      </div>
      <Show when={hasChildren() && isExpanded()}>
        <For each={props.node.children}>
          {(child) => (
            <TaskNodeRow
              node={child}
              depth={props.depth + 1}
              expanded={props.expanded}
              selectedId={props.selectedId}
              onToggle={props.onToggle}
              onSelect={props.onSelect}
              onContextMenu={props.onContextMenu}
              registerRef={props.registerRef}
            />
          )}
        </For>
      </Show>
    </div>
  );
}

function updateTaskQuery(id: string) {
  const url = new URL(window.location.href);
  url.searchParams.set("task", id);
  history.replaceState({}, "", url);
}

function getInitialTaskId(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get("task") ?? "";
}

function findPath(nodes: TaskNode[], id: string, trail: string[] = []): string[] {
  for (const node of nodes) {
    const nextTrail = [...trail, node.id];
    if (node.id === id) return nextTrail;
    const childPath = findPath(node.children ?? [], id, nextTrail);
    if (childPath.length > 0) return childPath;
  }
  return [];
}

function updateNode(nodes: TaskNode[], id: string, updater: (node: TaskNode) => TaskNode): TaskNode[] {
  return nodes.map((node) => {
    if (node.id === id) {
      return updater(node);
    }
    if (node.children && node.children.length > 0) {
      return { ...node, children: updateNode(node.children, id, updater) };
    }
    return node;
  });
}

function countTasks(nodes: TaskNode[], status?: TaskStatus): number {
  return nodes.reduce((total, node) => {
    const matches = status ? node.status === status : true;
    return total + (matches ? 1 : 0) + countTasks(node.children ?? [], status);
  }, 0);
}
