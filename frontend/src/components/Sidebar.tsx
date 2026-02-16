import { Link } from "@tanstack/solid-router"
import { BsSliders, BsViewList, BsTerminal } from "solid-icons/bs"
import { FaSolidTasks } from "solid-icons/fa"
import { IoSettingsSharp } from "solid-icons/io"
import { RiSystemDashboardHorizontalFill } from "solid-icons/ri"

/**
 * Sidebar Navigation Component
 *
 * Responsive sidebar with:
 * - Primary navigation routes
 * - Active route highlighting
 * - Mobile collapse behavior (icons only)
 * - Desktop expanded view
 * - Uses design tokens for consistent styling
 */
export default function Sidebar() {
  return (
    <aside class="sidebar" aria-label="Primary Navigation">
      <div class="sidebar-brand">OrbitMesh</div>
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
        <Link to="/settings" class={`nav-item`}>
          <IoSettingsSharp class="nav-icon" />
          <span class="nav-label">Settings</span>
        </Link>
      </nav>
    </aside>
  )
}
