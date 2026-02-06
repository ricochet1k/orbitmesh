import type { CommitSummary, TaskNode } from "../types/api";
import type { GraphLink, GraphNode } from "./types";

interface GraphData {
  nodes: GraphNode[];
  links: GraphLink[];
}

export function buildTaskGraph(tasks: TaskNode[]): GraphData {
  const nodes: GraphNode[] = [];
  const links: GraphLink[] = [];

  if (tasks.length === 0) {
    return { nodes, links };
  }

  nodes.push({ id: "task-root", type: "agent", label: "Task Tree" });

  const walk = (node: TaskNode, parentId?: string) => {
    nodes.push({ id: node.id, type: "task", label: node.title, status: node.status });
    if (parentId) {
      links.push({ source: parentId, target: node.id });
    }
    (node.children ?? []).forEach((child) => walk(child, node.id));
  };

  tasks.forEach((task) => {
    walk(task, "task-root");
  });

  return { nodes, links };
}

export function buildCommitGraph(commits: CommitSummary[]): GraphData {
  const nodes: GraphNode[] = [];
  const links: GraphLink[] = [];

  if (commits.length === 0) {
    return { nodes, links };
  }

  nodes.push({ id: "commit-root", type: "agent", label: "Commit History" });
  commits.forEach((commit) => {
    nodes.push({ id: commit.sha, type: "commit", label: commit.message });
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
