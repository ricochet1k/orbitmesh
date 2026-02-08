import type { CommitSummary, TaskNode } from "../types/api";
import type { GraphLink, GraphNode } from "./types";

interface GraphData {
  nodes: GraphNode[];
  links: GraphLink[];
}

/** Map TaskStatus to a numeric priority (higher = more important). */
function taskPriority(status?: string): number {
  switch (status) {
    case "in_progress": return 3;
    case "pending":     return 2;
    case "completed":   return 1;
    default:            return 2;
  }
}

export function buildTaskGraph(tasks: TaskNode[]): GraphData {
  const nodes: GraphNode[] = [];
  const links: GraphLink[] = [];

  if (tasks.length === 0) {
    return { nodes, links };
  }

  nodes.push({ id: "task-root", type: "agent", label: "Task Tree", depth: 0, priority: 4 });

  const walk = (node: TaskNode, parentId: string, depth: number) => {
    nodes.push({
      id: node.id,
      type: "task",
      label: node.title,
      status: node.status,
      depth,
      priority: taskPriority(node.status),
    });
    links.push({ source: parentId, target: node.id });
    (node.children ?? []).forEach((child) => walk(child, node.id, depth + 1));
  };

  tasks.forEach((task) => {
    walk(task, "task-root", 1);
  });

  return { nodes, links };
}

export function buildCommitGraph(commits: CommitSummary[]): GraphData {
  const nodes: GraphNode[] = [];
  const links: GraphLink[] = [];

  if (commits.length === 0) {
    return { nodes, links };
  }

  nodes.push({ id: "commit-root", type: "agent", label: "Commit History", depth: 0, priority: 4 });
  commits.forEach((commit, i) => {
    nodes.push({
      id: commit.sha,
      type: "commit",
      label: commit.message,
      depth: 1,
      priority: commits.length - i,   // newer commits = higher priority
    });
    links.push({ source: "commit-root", target: commit.sha });
  });

  return { nodes, links };
}

export function buildUnifiedGraph(tasks: TaskNode[], commits: CommitSummary[]): GraphData {
  const taskGraph = buildTaskGraph(tasks);
  const commitGraph = buildCommitGraph(commits);

  return {
    nodes: [...taskGraph.nodes, ...commitGraph.nodes],
    links: [...taskGraph.links, ...commitGraph.links],
  };
}
