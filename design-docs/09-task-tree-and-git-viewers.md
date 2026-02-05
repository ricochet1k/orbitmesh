# Task Tree & Git Viewers Design

## Overview
OrbitMesh provides two key views for tracking progress and history: a hierarchical Task Tree and a Git Commit Viewer. This document outlines their design and integration.

## Interface Components

### Hierarchical Task Tree
- **View**: `/tasks/tree`
- **Features**:
  - Nested display of epics, tasks, and subtasks.
  - Collapsible nodes for focused viewing.
  - Status indicators (badges) for each node (Pending, In Progress, Completed).
  - Search and filtering by role and status.

### Git Commit Viewer
- **View**: `/history/commits`
- **Features**:
  - List of commits with author, message, and timestamp.
  - Integration with agent sessions: see which agent produced which commit.
  - Commit Diff Viewer: side-by-side view of file changes.

## Integration with Force-Directed Graph
- **Interactivity**: Clicking a node in the graph can jump to its entry in the task tree or commit viewer.
- **Bi-directional Sync**: Selecting a task in the tree highlights it in the system graph.

## Data Fetching Strategy
- **Task Tree**: Fetch full hierarchy via `GET /api/v1/tasks/tree` (to be implemented).
- **Commits**: Fetch git history via a new PTY-based git provider or direct backend git access.
- **SSE**: Both views update in real-time as tasks are created/updated and commits are made.

## Navigation & Interaction
- **Context Menu**: Right-click on a task to quickly add a subtask or change its status.
- **Deep Linking**: Support for direct URLs to specific tasks or commits.
