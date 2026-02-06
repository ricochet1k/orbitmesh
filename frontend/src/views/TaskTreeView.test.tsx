import { render, screen, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import TaskTreeView from "./TaskTreeView";
import { apiClient } from "../api/client";

vi.mock("../api/client", () => ({
  apiClient: {
    getTaskTree: vi.fn(),
  }
}));

vi.mock("../graph/AgentGraph", () => ({
  default: () => <div data-testid="agent-graph" />,
}));

const taskTreePayload = {
  tasks: [
    {
      id: "task-1",
      title: "Parent Task",
      role: "developer",
      status: "pending",
      updated_at: "2026-02-05T12:00:00Z",
      children: [
        {
          id: "task-1-1",
          title: "Telemetry review",
          role: "reviewer-reliability",
          status: "in_progress",
          updated_at: "2026-02-05T12:10:00Z",
        },
      ],
    },
    {
      id: "task-2",
      title: "Docs handoff",
      role: "documentation",
      status: "completed",
      updated_at: "2026-02-05T12:20:00Z",
    },
  ],
};

describe("TaskTreeView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.getTaskTree as any).mockResolvedValue(taskTreePayload);
    window.history.replaceState({}, "", "/");
    (window.HTMLElement.prototype as any).scrollIntoView = vi.fn();
  });

  it("renders tasks from the API", async () => {
    render(() => <TaskTreeView />);

    expect(await screen.findByText("Parent Task")).toBeDefined();
    expect(screen.getByText("Telemetry review")).toBeDefined();
    expect(screen.getByText("Docs handoff")).toBeDefined();
    expect(screen.getByTestId("agent-graph")).toBeDefined();
  });

  it("filters by role and status", async () => {
    render(() => <TaskTreeView />);
    await screen.findByText("Parent Task");

    const roleSelect = screen.getByDisplayValue("All roles");
    fireEvent.change(roleSelect, { target: { value: "documentation" } });

    const statusSelect = screen.getByDisplayValue("All status");
    fireEvent.change(statusSelect, { target: { value: "completed" } });

    expect(screen.getByText("Docs handoff")).toBeDefined();
    expect(screen.queryByText("Parent Task")).toBeNull();
  });

  it("keeps parents when a child matches search", async () => {
    render(() => <TaskTreeView />);
    await screen.findByText("Parent Task");

    const searchInput = screen.getByPlaceholderText("Search tasks");
    fireEvent.input(searchInput, { target: { value: "Telemetry" } });

    expect(screen.getByText("Parent Task")).toBeDefined();
    expect(screen.getByText("Telemetry review")).toBeDefined();
    expect(screen.queryByText("Docs handoff")).toBeNull();
  });

});
