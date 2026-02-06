import { render, screen, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import CommitHistoryView from "./CommitHistoryView";
import { apiClient } from "../api/client";

vi.mock("../api/client", () => ({
  apiClient: {
    listCommits: vi.fn(),
    getCommit: vi.fn(),
  }
}));

vi.mock("../graph/AgentGraph", () => ({
  default: () => <div data-testid="agent-graph" />,
}));

const commitListPayload = {
  commits: [
    {
      sha: "abc1234",
      message: "Initial commit",
      author: "Ada Lovelace",
      email: "ada@example.com",
      timestamp: "2026-02-05T12:00:00Z",
    },
    {
      sha: "def5678",
      message: "Add task tree",
      author: "Grace Hopper",
      email: "grace@example.com",
      timestamp: "2026-02-05T13:00:00Z",
    },
  ],
};

const commitDetailMap: Record<string, any> = {
  abc1234: {
    commit: {
      sha: "abc1234",
      message: "Initial commit",
      author: "Ada Lovelace",
      email: "ada@example.com",
      timestamp: "2026-02-05T12:00:00Z",
      diff: "diff --git a/demo.txt b/demo.txt\n@@ -0,0 +1 @@\n+hello\n",
      files: ["demo.txt"],
    },
  },
  def5678: {
    commit: {
      sha: "def5678",
      message: "Add task tree",
      author: "Grace Hopper",
      email: "grace@example.com",
      timestamp: "2026-02-05T13:00:00Z",
      diff: "diff --git a/tree.txt b/tree.txt\n@@ -1 +1,2 @@\n-root\n+root\n+child\n",
      files: ["tree.txt"],
    },
  },
};

describe("CommitHistoryView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.listCommits as any).mockResolvedValue(commitListPayload);
    (apiClient.getCommit as any).mockImplementation((sha: string) =>
      Promise.resolve(commitDetailMap[sha])
    );
    window.history.replaceState({}, "", "/history/commits");
  });

  it("renders commit list and shows initial diff", async () => {
    render(() => <CommitHistoryView />);

    expect(await screen.findByText("Initial commit")).toBeDefined();
    expect(screen.getByText("Ada Lovelace")).toBeDefined();
    expect(await screen.findByText("demo.txt", { selector: ".commit-files span" })).toBeDefined();
    expect(await screen.findByText(/hello/)).toBeDefined();
    expect(screen.getByTestId("agent-graph")).toBeDefined();
  });

  it("updates detail when selecting a commit", async () => {
    render(() => <CommitHistoryView />);
    await screen.findByText("Initial commit");

    fireEvent.click(screen.getByText("Add task tree"));

    expect(await screen.findByText(/Grace Hopper/, { selector: ".commit-sub" })).toBeDefined();
    expect(await screen.findByText("tree.txt", { selector: ".commit-files span" })).toBeDefined();
    expect(await screen.findByText(/child/)).toBeDefined();
  });
});
