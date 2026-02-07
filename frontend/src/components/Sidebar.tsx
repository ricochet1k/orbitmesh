import { createMemo, For } from "solid-js";

interface NavItem {
  label: string;
  href: string;
  match: (value: string) => boolean;
  icon: (props: { class?: string }) => any;
}

interface SidebarProps {
  currentPath: string;
  onNavigate: (href: string) => void;
  navItems: NavItem[];
}

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
export default function Sidebar(props: SidebarProps) {
  const activeItems = createMemo(() => {
    return props.navItems.map(item => ({
      ...item,
      isActive: item.match(props.currentPath)
    }));
  });

  const handleNavClick = (event: MouseEvent, href: string) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    props.onNavigate(href);
  };

  return (
    <aside class="sidebar" aria-label="Primary Navigation">
      <div class="sidebar-brand">OrbitMesh</div>
      <nav class="sidebar-nav">
        <For each={activeItems()}>
          {(item) => (
            <a
              href={item.href}
              class={`nav-item ${item.isActive ? "active" : ""}`}
              aria-current={item.isActive ? "page" : undefined}
              onClick={(event) => handleNavClick(event, item.href)}
              title={item.label}
            >
              <span class="nav-icon" aria-hidden="true">
                <item.icon />
              </span>
              <span class="nav-label">{item.label}</span>
            </a>
          )}
        </For>
      </nav>
    </aside>
  );
}
