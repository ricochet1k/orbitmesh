import { createFileRoute } from '@tanstack/solid-router'
import { createEffect, createMemo, createResource, createSignal, For, Show, onCleanup } from "solid-js"
import { apiClient } from "../api/client"
import type { ProviderConfigResponse, SessionResponse, TaskNode, TaskStatus } from "../types/api"
import AgentGraph from "../graph/AgentGraph"
import { buildTaskGraph } from "../graph/graphData"
import EmptyState from "../components/EmptyState"
import SkeletonLoader from "../components/SkeletonLoader"
import { setDockSessionId } from "../state/agentDock"

export const Route = createFileRoute('/tasks')({
  component: TaskTreeView,
})
interface ContextMenuState {
  id: string
  x: number
  y: number
}

const statusLabels: Record<TaskStatus, string> = {
  pending: "Pending",
  in_progress: "In Progress",
  completed: "Completed",
}

interface TaskTreeViewProps {
  onNavigate?: (path: string) => void
}

const typeOptions = [
  { value: "adk", label: "ADK (Google)", providerType: "adk" },
  { value: "pty", label: "PTY (Claude)", providerType: "pty" },
  { value: "anthropic", label: "Anthropic", providerType: "anthropic" },
  { value: "openai", label: "OpenAI", providerType: "openai" },
]

export default function TaskTreeView(props: TaskTreeViewProps = {}) {
  const [treeResponse] = createResource(apiClient.getTaskTree)
  const [providers] = createResource(apiClient.listProviders)
  const [treeData, setTreeData] = createSignal<TaskNode[]>([])
  const [search, setSearch] = createSignal("")
  const [roleFilter, setRoleFilter] = createSignal("all")
  const [statusFilter, setStatusFilter] = createSignal("all")
  const [expanded, setExpanded] = createSignal<Set<string>>(new Set())
  const [autoExpanded, setAutoExpanded] = createSignal(false)
  const [menu, setMenu] = createSignal<ContextMenuState | null>(null)
  const [selectedId, setSelectedId] = createSignal(getInitialTaskId())
  const [providerChoice, setProviderChoice] = createSignal(typeOptions[0]?.value ?? "type:adk")
  const [providerInitialized, setProviderInitialized] = createSignal(false)
  const [startState, setStartState] = createSignal<"idle" | "starting" | "success" | "error">("idle")
  const [startError, setStartError] = createSignal<string | null>(null)
  const [sessionInfo, setSessionInfo] = createSignal<{ taskId: string; session: SessionResponse } | null>(null)

  const nodeRefs = new Map<string, HTMLDivElement>()

  const providerConfigs = () => providers()?.providers ?? []

  const providerOptions = createMemo(() => {
    const options: Array<{
      value: string
      label: string
      providerType: string
      providerId?: string
    }> = []
    providerConfigs().forEach((provider: ProviderConfigResponse) => {
      const inactive = provider.is_active ? "" : " (inactive)"
      options.push({
        value: `config:${provider.id}`,
        label: `${provider.name} (${provider.type})${inactive}`,
        providerType: provider.type,
        providerId: provider.id,
      })
    })
    return [...options, ...typeOptions]
  })

  const selectedProvider = createMemo(() =>
    providerOptions().find((option) => option.value === providerChoice()) ?? providerOptions()[0],
  )

  createEffect(() => {
    if (treeResponse()) {
      setTreeData(treeResponse()?.tasks ?? [])
    }
  })

  createEffect(() => {
    if (providerInitialized()) return
    if (providerConfigs().length > 0) {
      setProviderChoice(`config:${providerConfigs()[0].id}`)
    }
    setProviderInitialized(true)
  })

  createEffect(() => {
    if (treeData().length === 0 || expanded().size > 0 || autoExpanded()) return
    const next = new Set<string>()
    treeData().forEach((task) => next.add(task.id))
    setExpanded(next)
    setAutoExpanded(true)
  })

  createEffect(() => {
    const id = selectedId()
    if (!id) return
    const path = findPath(treeData(), id)
    if (path.length > 0) {
      const next = new Set(expanded())
      let changed = false
      path.forEach((nodeId) => {
        if (!next.has(nodeId)) {
          next.add(nodeId)
          changed = true
        }
      })
      if (changed) {
        setExpanded(next)
      }
    }
    const node = nodeRefs.get(id)
    if (node) {
      requestAnimationFrame(() => {
        node.scrollIntoView({ block: "center", behavior: "smooth" })
      })
    }
  })

  createEffect(() => {
    const id = selectedId()
    if (!id) return
    if (sessionInfo()?.taskId && sessionInfo()?.taskId !== id) {
      setSessionInfo(null)
    }
    setStartError(null)
    setStartState("idle")
  })

  createEffect(() => {
    const handleClick = () => setMenu(null)
    window.addEventListener("click", handleClick)
    onCleanup(() => window.removeEventListener("click", handleClick))
  })

  const roles = createMemo(() => {
    const unique = new Set<string>()
    const collect = (nodes: TaskNode[]) => {
      nodes.forEach((node) => {
        unique.add(node.role)
        collect(node.children ?? [])
      })
    }
    collect(treeData())
    return Array.from(unique.values()).sort()
  })

  const filteredTree = createMemo(() => {
    const query = search().trim().toLowerCase()
    const role = roleFilter()
    const status = statusFilter()

    const matches = (node: TaskNode) => {
      const roleMatches = role === "all" || node.role === role
      const statusMatches = status === "all" || node.status === status
      const queryMatches = query === "" || node.title.toLowerCase().includes(query)
      return roleMatches && statusMatches && queryMatches
    }

    const filterNodes = (nodes: TaskNode[]): TaskNode[] => {
      return nodes.flatMap((node) => {
        const children = filterNodes(node.children ?? [])
        if (matches(node) || children.length > 0) {
          return [{ ...node, children }]
        }
        return []
      })
    }

    return filterNodes(treeData())
  })

  const graphData = createMemo(() => buildTaskGraph(treeData()))
  const selectedTask = createMemo(() => findTaskById(treeData(), selectedId()))

  const toggleExpanded = (id: string) => {
    const next = new Set(expanded())
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    setExpanded(next)
  }

  const selectTask = (id: string) => {
    setSelectedId(id)
    updateTaskQuery(id)
  }

  const onContextMenu = (event: MouseEvent, id: string) => {
    event.preventDefault()
    setMenu({ id, x: event.clientX, y: event.clientY })
  }

  const updateStatus = (id: string, status: TaskStatus) => {
    setTreeData((prev) => updateNode(prev, id, (node) => ({
      ...node,
      status,
      updated_at: new Date().toISOString(),
    })))
    setMenu(null)
  }

  const addSubtask = (id: string) => {
    const title = window.prompt("Subtask title")
    if (!title) return
    const newNode: TaskNode = {
      id: `task-${Date.now().toString(36)}`,
      title,
      role: "developer",
      status: "pending",
      updated_at: new Date().toISOString(),
      children: [],
    }
    setTreeData((prev) => updateNode(prev, id, (node) => ({
      ...node,
      children: [...(node.children ?? []), newNode],
      updated_at: new Date().toISOString(),
    })))
    const next = new Set(expanded())
    next.add(id)
    setExpanded(next)
    setMenu(null)
  }

  const startAgent = async () => {
    const task = selectedTask()
    if (!task) return
    setStartState("starting")
    setStartError(null)
    try {
      const session = await apiClient.createTaskSession({
        taskId: task.id,
        taskTitle: task.title,
        providerType: selectedProvider()?.providerType,
        providerId: selectedProvider()?.providerId,
      })
      setSessionInfo({ taskId: task.id, session })
      setDockSessionId(session.id)
      setStartState("success")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to start session."
      setStartError(message)
      setStartState("error")
    }
  }

  const openSessionViewer = (id: string) => {
    if (props.onNavigate) {
      props.onNavigate(`/sessions/${id}`)
      return
    }
    window.location.assign(`/sessions/${id}`)
  }

  const dismissSessionInfo = () => {
    setSessionInfo(null)
    setStartState("idle")
    setStartError(null)
  }

  return (
    <div class="task-tree-view" data-testid="tasks-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Task operations</p>
          <h1 data-testid="tasks-heading">Task Tree</h1>
          <p class="dashboard-subtitle">
            Explore hierarchical task progress, filter by role or status, and sync selections with the system graph.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card" data-testid="tasks-meta-tracked">
            <p>Tasks tracked</p>
            <strong>{countTasks(treeData())}</strong>
          </div>
          <div class="meta-card" data-testid="tasks-meta-in-progress">
            <p>In progress</p>
            <strong>{countTasks(treeData(), "in_progress")}</strong>
          </div>
          <div class="meta-card" data-testid="tasks-meta-completed">
            <p>Completed</p>
            <strong>{countTasks(treeData(), "completed")}</strong>
          </div>
        </div>
      </header>

      <main class="task-tree-layout">
        <section class="dashboard-panel task-tree-panel">
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
              data-testid="tasks-search"
              value={search()}
              onInput={(event) => setSearch(event.currentTarget.value)}
            />
            <select
              value={roleFilter()}
              data-testid="tasks-role-filter"
              onChange={(event) => setRoleFilter(event.currentTarget.value)}
            >
              <option value="all">All roles</option>
              <For each={roles()}>{(role) => <option value={role}>{role}</option>}</For>
            </select>
            <select
              value={statusFilter()}
              data-testid="tasks-status-filter"
              onChange={(event) => setStatusFilter(event.currentTarget.value)}
            >
              <option value="all">All status</option>
              <option value="pending">Pending</option>
              <option value="in_progress">In Progress</option>
              <option value="completed">Completed</option>
            </select>
          </div>

          <Show 
            when={!treeResponse.loading} 
            fallback={<SkeletonLoader variant="list" count={8} />}
          >
            <Show 
              when={treeData().length > 0}
              fallback={
                <EmptyState
                  icon="📋"
                  title="No tasks available"
                  description="The task tree is empty. Create your first task to start organizing your work."
                  variant="info"
                />
              }
            >
              <div class="task-tree" data-testid="task-tree">
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
                <Show when={filteredTree().length === 0 && treeData().length > 0}>
                  <EmptyState
                    icon="🔍"
                    title="No matching tasks"
                    description="Try adjusting your search or filter criteria to find tasks."
                    variant="info"
                    action={{
                      label: "Clear Filters",
                      onClick: () => {
                        setSearch("")
                        setRoleFilter("all")
                        setStatusFilter("all")
                      }
                    }}
                  />
                </Show>
              </div>
            </Show>
          </Show>
        </section>

        <section class="dashboard-panel task-detail-panel" data-testid="task-detail-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Task control</p>
              <h2>Agent Launchpad</h2>
            </div>
            <span class="panel-pill neutral">Ready</span>
          </div>
          <Show when={!treeResponse.loading && !treeResponse.error} fallback={<p class="muted">Loading task focus...</p>}>
            <Show when={selectedTask()} fallback={<p class="empty-state">Select a task to start an agent session.</p>}>
              {(task) => (
                <div class="task-detail" data-testid="task-details">
                  <div class="task-detail-card">
                    <div>
                      <span>Task</span>
                      <strong>{task().title}</strong>
                    </div>
                    <div>
                      <span>Task ID</span>
                      <strong>{task().id}</strong>
                    </div>
                    <div>
                      <span>Role</span>
                      <strong>{task().role}</strong>
                    </div>
                    <div>
                      <span>Status</span>
                      <strong class={`task-status ${task().status}`}>{statusLabels[task().status]}</strong>
                    </div>
                    <div>
                      <span>Updated</span>
                      <strong>{new Date(task().updated_at).toLocaleString()}</strong>
                    </div>
                  </div>
                  <div class="task-start-controls">
                    <label>
                      Agent profile
                      <select value={providerChoice()} onChange={(event) => setProviderChoice(event.currentTarget.value)}>
                        <Show when={providerConfigs().length > 0}>
                          <optgroup label="Saved providers">
                            <For each={providerConfigs()}>
                              {(provider) => (
                                <option value={`config:${provider.id}`}>
                                  {provider.name} ({provider.type}){provider.is_active ? "" : " (inactive)"}
                                </option>
                              )}
                            </For>
                          </optgroup>
                        </Show>
                        <optgroup label="Provider types">
                          <For each={typeOptions}>
                            {(option) => <option value={option.value}>{option.label}</option>}
                          </For>
                        </optgroup>
                      </select>
                    </label>
                    <button type="button" onClick={startAgent} disabled={startState() === "starting"}>
                      {startState() === "starting" ? "Starting..." : "Start agent"}
                    </button>
                  </div>
                  <Show when={startError()}>
                    {(message) => <p class="notice-banner error">{message()}</p>}
                  </Show>
                  <Show
                    when={sessionInfo() && sessionInfo()?.taskId === task().id ? sessionInfo() : null}
                  >
                    {(info) => (
                      <div class="session-launch-card" data-testid="session-launch-card">
                        <div>
                          <p class="muted">Session ready</p>
                          <strong>{info().session.id}</strong>
                        </div>
                        <div class="session-launch-meta">
                          <span class={`state-badge ${info().session.state}`}>{info().session.state.replace("_", " ")}</span>
                          <button type="button" onClick={() => openSessionViewer(info().session.id)}>
                            Open Session Viewer
                          </button>
                          <button type="button" class="neutral" onClick={dismissSessionInfo} title="Dismiss">
                            ✕
                          </button>
                        </div>
                      </div>
                    )}
                  </Show>
                </div>
              )}
            </Show>
          </Show>
          <Show when={treeResponse.error}>
            <p class="notice-banner error">Unable to load tasks. Check the API connection.</p>
          </Show>
        </section>

        <section class="dashboard-panel graph-view compact">
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
  )
}

interface TaskNodeRowProps {
  node: TaskNode
  depth: number
  expanded: () => Set<string>
  selectedId: () => string
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  onContextMenu: (event: MouseEvent, id: string) => void
  registerRef: Map<string, HTMLDivElement>
}

function TaskNodeRow(props: TaskNodeRowProps) {
  const isExpanded = () => props.expanded().has(props.node.id)
  const hasChildren = () => (props.node.children ?? []).length > 0

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
            event.stopPropagation()
            if (hasChildren()) props.onToggle(props.node.id)
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
  )
}

function updateTaskQuery(id: string) {
  const url = new URL(window.location.href)
  url.searchParams.set("task", id)
  history.replaceState({}, "", url)
}

function getInitialTaskId(): string {
  const params = new URLSearchParams(window.location.search)
  return params.get("task") ?? ""
}

function findPath(nodes: TaskNode[], id: string, trail: string[] = []): string[] {
  for (const node of nodes) {
    const nextTrail = [...trail, node.id]
    if (node.id === id) return nextTrail
    const childPath = findPath(node.children ?? [], id, nextTrail)
    if (childPath.length > 0) return childPath
  }
  return []
}

function findTaskById(nodes: TaskNode[], id: string): TaskNode | null {
  for (const node of nodes) {
    if (node.id === id) return node
    const childMatch = findTaskById(node.children ?? [], id)
    if (childMatch) return childMatch
  }
  return null
}

function updateNode(nodes: TaskNode[], id: string, updater: (node: TaskNode) => TaskNode): TaskNode[] {
  return nodes.map((node) => {
    if (node.id === id) {
      return updater(node)
    }
    if (node.children && node.children.length > 0) {
      return { ...node, children: updateNode(node.children, id, updater) }
    }
    return node
  })
}

function countTasks(nodes: TaskNode[], status?: TaskStatus): number {
  return nodes.reduce((total, node) => {
    const matches = status ? node.status === status : true
    return total + (matches ? 1 : 0) + countTasks(node.children ?? [], status)
  }, 0)
}
