import { fireEvent, render, screen } from "@solidjs/testing-library";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TasksRoute from "./tasks";

const mockNavigate = vi.fn();

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => mockNavigate,
}));

vi.mock("../graph/AgentGraph", () => ({
  default: () => <div data-testid="mock-agent-graph">Graph View</div>,
}));

vi.mock("../api/client", () => ({
  apiClient: {
    getTaskTree: vi.fn().mockResolvedValue({
      tasks: [
        {
          id: "task-1",
          title: "Tighten task IA",
          role: "developer",
          status: "in_progress",
          updated_at: "2026-03-26T10:00:00Z",
          children: [],
        },
      ],
    }),
    listAgents: vi.fn().mockResolvedValue({
      agents: [{ id: "agent-1", name: "Frontend Agent" }],
    }),
    createTaskSession: vi.fn(),
  },
}));

describe("Tasks route smoke", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState({}, "", "/tasks");
  });

  it("hides graph sync by default and reveals it in advanced mode", async () => {
    render(() => <TasksRoute />);

    expect(await screen.findByTestId("tasks-view")).toBeDefined();
    expect(screen.getByTestId("tasks-graph-collapsed")).toBeDefined();
    expect(screen.queryByTestId("tasks-graph-panel")).toBeNull();
    expect(screen.queryByTestId("mock-agent-graph")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Show Advanced" }));

    expect(await screen.findByTestId("tasks-graph-panel")).toBeDefined();
    expect(screen.getByTestId("mock-agent-graph")).toBeDefined();
    expect(screen.queryByTestId("tasks-graph-collapsed")).toBeNull();
  });
});
