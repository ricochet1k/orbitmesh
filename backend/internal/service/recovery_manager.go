package service

import "context"

type recoveryManager struct {
	executor *AgentExecutor
}

func newRecoveryManager(executor *AgentExecutor) *recoveryManager {
	return &recoveryManager{executor: executor}
}

// OnStartup is a no-op scaffold for upcoming recovery wiring.
func (r *recoveryManager) OnStartup(ctx context.Context) error {
	_ = ctx
	if r == nil || r.executor == nil {
		return nil
	}
	return nil
}
