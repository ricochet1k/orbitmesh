package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type AllowedSubAgentsKey struct{}

func WithAllowedSubAgents(ctx context.Context, agents []string) context.Context {
	return context.WithValue(ctx, AllowedSubAgentsKey{}, agents)
}

func AllowedSubAgentsFromContext(ctx context.Context) []string {
	v, ok := ctx.Value(AllowedSubAgentsKey{}).([]string)
	if !ok {
		return nil
	}
	return v
}

type SubAgentSpawnerDelegate interface {
	SpawnAndAwait(ctx context.Context, agentID, projectID, task string) (string, error)
}

var SubAgentDelegate SubAgentSpawnerDelegate

func RegisterSubAgentDelegate(d SubAgentSpawnerDelegate) {
	SubAgentDelegate = d
}

func RegisterSpawnSubAgent(reg Registry) error {
	def := ToolDef{
		Name:        "spawn_sub_agent",
		Description: "Spawns a new sub-agent session with a specific agent ID and task, then waits for it to complete.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string","description":"The ID of the agent to spawn"},"task":{"type":"string","description":"The task for the sub-agent to perform"}},"required":["agent_id","task"]}`),
		Handler:     spawnSubAgentHandle,
	}
	if err := reg.Register(def); err != nil && !strings.Contains(err.Error(), "already registered") {
		return err
	}
	return nil
}

func spawnSubAgentHandle(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		AgentID string `json:"agent_id"`
		Task    string `json:"task"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}

	if strings.TrimSpace(args.Task) == "" {
		return "", errors.New("task is required")
	}

	allowedAgents := AllowedSubAgentsFromContext(ctx)
	if allowedAgents != nil {
		isAllowed := false
		for _, a := range allowedAgents {
			if a == args.AgentID {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return "", fmt.Errorf("agent_id '%s' is not in the allowed list for this tool", args.AgentID)
		}
	} else {
		return "", fmt.Errorf("spawn_sub_agent tool cannot be run without an allowed sub-agents configuration")
	}

	if SubAgentDelegate == nil {
		return "", errors.New("sub-agent spawner delegate not configured")
	}

	projectID := strings.TrimSpace(ProjectIDFromContext(ctx))
	return SubAgentDelegate.SpawnAndAwait(ctx, args.AgentID, projectID, args.Task)
}
