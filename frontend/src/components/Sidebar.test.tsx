import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import Sidebar from "./Sidebar";

// Mock the router Link component
vi.mock("@tanstack/solid-router", () => ({
  Link: (props: any) => (
    <a href={props.to} class={props.class} data-testid={`link-${props.to}`}>
      {props.children}
    </a>
  ),
}));

// Mock the icons
vi.mock("solid-icons/bs", () => ({
  BsSliders: () => <span data-testid="icon-sliders" />,
  BsViewList: () => <span data-testid="icon-list" />,
  BsTerminal: () => <span data-testid="icon-terminal" />,
}));

vi.mock("solid-icons/fa", () => ({
  FaSolidTasks: () => <span data-testid="icon-tasks" />,
}));

vi.mock("solid-icons/io", () => ({
  IoSettingsSharp: () => <span data-testid="icon-settings" />,
}));

vi.mock("solid-icons/ri", () => ({
  RiSystemDashboardHorizontalFill: () => <span data-testid="icon-dashboard" />,
}));

describe("Sidebar Navigation", () => {
  it("renders the brand name", () => {
    render(() => <Sidebar />);
    expect(screen.getByText("OrbitMesh")).toBeDefined();
  });

  it("renders all primary navigation links", () => {
    render(() => <Sidebar />);

    expect(screen.getByText("Dashboard")).toBeDefined();
    expect(screen.getByText("Tasks")).toBeDefined();
    expect(screen.getByText("Sessions")).toBeDefined();
    expect(screen.getByText("Terminals")).toBeDefined();
    expect(screen.getByText("Extractors")).toBeDefined();
    expect(screen.getByText("Settings")).toBeDefined();
  });

  it("renders navigation icons for each route", () => {
    render(() => <Sidebar />);

    expect(screen.getByTestId("icon-dashboard")).toBeDefined();
    expect(screen.getByTestId("icon-tasks")).toBeDefined();
    expect(screen.getByTestId("icon-list")).toBeDefined();
    expect(screen.getByTestId("icon-terminal")).toBeDefined();
    expect(screen.getByTestId("icon-sliders")).toBeDefined();
    expect(screen.getByTestId("icon-settings")).toBeDefined();
  });

  it("links to correct routes", () => {
    render(() => <Sidebar />);

    const dashboardLink = screen.getByTestId("link-/") as HTMLAnchorElement;
    expect(dashboardLink.href).toContain("/");

    const tasksLink = screen.getByTestId("link-/tasks") as HTMLAnchorElement;
    expect(tasksLink.href).toContain("/tasks");

    const sessionsLink = screen.getByTestId("link-/sessions") as HTMLAnchorElement;
    expect(sessionsLink.href).toContain("/sessions");

    const extractorsLink = screen.getByTestId("link-/extractors") as HTMLAnchorElement;
    expect(extractorsLink.href).toContain("/extractors");

    const terminalsLink = screen.getByTestId("link-/terminals") as HTMLAnchorElement;
    expect(terminalsLink.href).toContain("/terminals");

    const settingsLink = screen.getByTestId("link-/settings/providers") as HTMLAnchorElement;
    expect(settingsLink.href).toContain("/settings");
  });

  it("has proper ARIA label for accessibility", () => {
    render(() => <Sidebar />);
    const nav = screen.getByRole("complementary") as HTMLElement;
    expect(nav.getAttribute("aria-label")).toBe("Primary Navigation");
  });

  it("renders sidebar with proper semantic structure", () => {
    render(() => <Sidebar />);
    const aside = screen.getByRole("complementary") as HTMLElement;
    expect(aside.classList.contains("sidebar")).toBe(true);
  });

  it("renders navigation elements with proper classes", () => {
    const { container } = render(() => <Sidebar />);
    
    const navContainer = container.querySelector(".sidebar-nav");
    expect(navContainer).toBeDefined();
    
    const navItems = container.querySelectorAll(".nav-item");
    expect(navItems.length).toBe(6);
  });

  it("displays nav labels for each navigation item", () => {
    const { container } = render(() => <Sidebar />);
    
    const navLabels = container.querySelectorAll(".nav-label");
    expect(navLabels.length).toBe(6);
    
    const labels = Array.from(navLabels).map(el => el.textContent);
    expect(labels).toContain("Dashboard");
    expect(labels).toContain("Tasks");
    expect(labels).toContain("Sessions");
    expect(labels).toContain("Terminals");
    expect(labels).toContain("Extractors");
    expect(labels).toContain("Settings");
  });
});
