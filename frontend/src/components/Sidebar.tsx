import { Link } from "@tanstack/solid-router"
import { BsSliders, BsViewList, BsTerminal } from "solid-icons/bs"
import { FaSolidTasks } from "solid-icons/fa"
import { IoSettingsSharp } from "solid-icons/io"
import { RiSystemDashboardHorizontalFill } from "solid-icons/ri"
import { createSignal, Show, For, onMount, onCleanup } from "solid-js"
import { useProjectStore } from "../state/project"
import type { ProjectResponse } from "../types/api"
import { apiClient } from "../api/client"

/**
 * Sidebar Navigation Component
 *
 * Responsive sidebar with:
 * - Project picker (dropdown + create form)
 * - Primary navigation routes
 * - Active route highlighting
 * - Mobile collapse behavior (icons only)
 * - Desktop expanded view
 * - Uses design tokens for consistent styling
 */
export default function Sidebar() {
  const projectStore = useProjectStore()

  const [dropdownOpen, setDropdownOpen] = createSignal(false)
  const [showCreateForm, setShowCreateForm] = createSignal(false)
  const [newName, setNewName] = createSignal("")
  const [newPath, setNewPath] = createSignal("")
  const [createError, setCreateError] = createSignal<string | null>(null)
  const [creating, setCreating] = createSignal(false)

  onMount(() => {
    projectStore.refresh()
  })

  // Close dropdown when clicking outside
  let dropdownRef: HTMLDivElement | undefined
  const handleOutsideClick = (e: MouseEvent) => {
    if (dropdownRef && !dropdownRef.contains(e.target as Node)) {
      setDropdownOpen(false)
    }
  }
  onMount(() => document.addEventListener("mousedown", handleOutsideClick))
  onCleanup(() => document.removeEventListener("mousedown", handleOutsideClick))

  const activeProject = () =>
    projectStore.projects().find((p) => p.id === projectStore.activeProjectId()) ?? null

  const selectProject = (id: string | null) => {
    projectStore.setActive(id)
    setDropdownOpen(false)
  }

  const openCreate = () => {
    setShowCreateForm(true)
    setNewName("")
    setNewPath("")
    setCreateError(null)
    setDropdownOpen(false)
  }

  const submitCreate = async (e: Event) => {
    e.preventDefault()
    setCreateError(null)
    setCreating(true)
    try {
      const proj = await apiClient.createProject({ name: newName(), path: newPath() })
      await projectStore.refresh()
      projectStore.setActive(proj.id)
      setShowCreateForm(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Failed to create project.")
    } finally {
      setCreating(false)
    }
  }

  const truncate = (s: string, max = 20) =>
    s.length > max ? s.slice(0, max - 1) + "…" : s

  return (
    <aside class="sidebar" aria-label="Primary Navigation">
      <div class="sidebar-brand">OrbitMesh</div>

      {/* Project Picker */}
      <div class="project-picker" ref={dropdownRef}>
        <div class="project-picker-row">
          <button
            class="project-picker-btn"
            onClick={() => setDropdownOpen((v) => !v)}
            title={activeProject()?.path ?? "All projects"}
          >
            <span class="project-picker-icon">▾</span>
            <span class="project-picker-name">
              {activeProject() ? truncate(activeProject()!.name) : "All projects"}
            </span>
          </button>
          <button
            class="project-picker-add"
            title="New project"
            onClick={openCreate}
          >
            +
          </button>
        </div>

        <Show when={dropdownOpen()}>
          <div class="project-dropdown">
            <button
              class={`project-dropdown-item${projectStore.activeProjectId() === null ? " active" : ""}`}
              onClick={() => selectProject(null)}
            >
              All projects
            </button>
            <For each={projectStore.projects()}>
              {(p: ProjectResponse) => (
                <button
                  class={`project-dropdown-item${projectStore.activeProjectId() === p.id ? " active" : ""}`}
                  onClick={() => selectProject(p.id)}
                  title={p.path}
                >
                  {truncate(p.name)}
                </button>
              )}
            </For>
            <Show when={projectStore.projects().length === 0 && !projectStore.isLoading()}>
              <span class="project-dropdown-empty">No projects yet</span>
            </Show>
          </div>
        </Show>

        <Show when={showCreateForm()}>
          <form class="project-create-form" onSubmit={submitCreate}>
            <input
              class="project-create-input"
              type="text"
              placeholder="Project name"
              value={newName()}
              onInput={(e) => setNewName(e.currentTarget.value)}
              required
            />
            <input
              class="project-create-input"
              type="text"
              placeholder="Directory path"
              value={newPath()}
              onInput={(e) => setNewPath(e.currentTarget.value)}
              required
            />
            <Show when={createError()}>
              <span class="project-create-error">{createError()}</span>
            </Show>
            <div class="project-create-actions">
              <button class="project-create-submit" type="submit" disabled={creating()}>
                {creating() ? "Creating…" : "Create"}
              </button>
              <button
                class="project-create-cancel"
                type="button"
                onClick={() => setShowCreateForm(false)}
              >
                Cancel
              </button>
            </div>
          </form>
        </Show>
      </div>

      <nav class="sidebar-nav">
        <Link to="/" class={`nav-item`}>
          <RiSystemDashboardHorizontalFill class="nav-icon" />
          <span class="nav-label">Dashboard</span>
        </Link>
        <Link to="/tasks" class={`nav-item`}>
          <FaSolidTasks class="nav-icon" />
          <span class="nav-label">Tasks</span>
        </Link>
        <Link to="/sessions" class={`nav-item`}>
          <BsViewList class="nav-icon" />
          <span class="nav-label">Sessions</span>
        </Link>
        <Link to="/terminals" class={`nav-item`}>
          <BsTerminal class="nav-icon" />
          <span class="nav-label">Terminals</span>
        </Link>
        <Link to="/extractors" class={`nav-item`}>
          <BsSliders class="nav-icon" />
          <span class="nav-label">Extractors</span>
        </Link>
        <Link to="/settings/providers" class={`nav-item`}>
          <IoSettingsSharp class="nav-icon" />
          <span class="nav-label">Settings</span>
        </Link>
      </nav>
    </aside>
  )
}
