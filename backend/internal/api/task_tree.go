package api

import (
	"encoding/json"
	"net/http"
	"time"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

func (h *Handler) tasksTree(w http.ResponseWriter, _ *http.Request) {
	tasks := sampleTaskTree()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiTypes.TaskTreeResponse{Tasks: tasks})
}

func sampleTaskTree() []apiTypes.TaskNode {
	now := time.Now()
	return []apiTypes.TaskNode{
		{
			ID:        "epic-operations",
			Title:     "Operations Readiness",
			Role:      "architect",
			Status:    apiTypes.TaskStatusInProgress,
			UpdatedAt: now.Add(-2 * time.Hour),
			Children: []apiTypes.TaskNode{
				{
					ID:        "task-guardrails",
					Title:     "Guardrail baselines",
					Role:      "developer",
					Status:    apiTypes.TaskStatusCompleted,
					UpdatedAt: now.Add(-5 * time.Hour),
					Children: []apiTypes.TaskNode{
						{
							ID:        "task-guardrails-copy",
							Title:     "Copy review for live rollout",
							Role:      "documentation",
							Status:    apiTypes.TaskStatusCompleted,
							UpdatedAt: now.Add(-4 * time.Hour),
						},
					},
				},
				{
					ID:        "task-telemetry",
					Title:     "Telemetry baselines",
					Role:      "developer",
					Status:    apiTypes.TaskStatusInProgress,
					UpdatedAt: now.Add(-45 * time.Minute),
					Children: []apiTypes.TaskNode{
						{
							ID:        "task-telemetry-alerts",
							Title:     "Alert routing",
							Role:      "reviewer-reliability",
							Status:    apiTypes.TaskStatusPending,
							UpdatedAt: now.Add(-30 * time.Minute),
						},
					},
				},
				{
					ID:        "task-incident-playbooks",
					Title:     "Incident playbooks",
					Role:      "documentation",
					Status:    apiTypes.TaskStatusPending,
					UpdatedAt: now.Add(-20 * time.Minute),
				},
			},
		},
		{
			ID:        "epic-execution",
			Title:     "Execution Fabric",
			Role:      "architect",
			Status:    apiTypes.TaskStatusPending,
			UpdatedAt: now.Add(-90 * time.Minute),
			Children: []apiTypes.TaskNode{
				{
					ID:        "task-tree-view",
					Title:     "Task tree viewer",
					Role:      "developer",
					Status:    apiTypes.TaskStatusInProgress,
					UpdatedAt: now.Add(-25 * time.Minute),
					Children: []apiTypes.TaskNode{
						{
							ID:        "task-tree-search",
							Title:     "Search + filters",
							Role:      "developer",
							Status:    apiTypes.TaskStatusInProgress,
							UpdatedAt: now.Add(-15 * time.Minute),
						},
						{
							ID:        "task-tree-context",
							Title:     "Context menu actions",
							Role:      "developer",
							Status:    apiTypes.TaskStatusPending,
							UpdatedAt: now.Add(-10 * time.Minute),
						},
					},
				},
				{
					ID:        "task-git-viewer",
					Title:     "Git commit viewer",
					Role:      "developer",
					Status:    apiTypes.TaskStatusPending,
					UpdatedAt: now.Add(-35 * time.Minute),
				},
				{
					ID:        "task-graph-sync",
					Title:     "Graph synchronization",
					Role:      "developer",
					Status:    apiTypes.TaskStatusPending,
					UpdatedAt: now.Add(-5 * time.Minute),
				},
			},
		},
	}
}
