package service

import (
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/domain"
	"github.com/ricochet1k/orbitmesh/internal/session"
)

const providerSwitchResumeSeedLimit = 24

func (e *AgentExecutor) buildResumeMessagesForProviderSwitch(sessionID, fromProviderType, toProviderType string) []session.Message {
	if !shouldAttachResumeMessagesForProviderSwitch(fromProviderType, toProviderType) {
		return nil
	}
	if e.resumeMsgBuilder == nil {
		return nil
	}

	history, err := e.GetSessionMessages(sessionID)
	if err != nil {
		return nil
	}

	messages := history.GetMessages()
	if len(messages) == 0 {
		return nil
	}

	window := make([]domain.Message, 0, providerSwitchResumeSeedLimit)
	for i := len(messages) - 1; i >= 0 && len(window) < providerSwitchResumeSeedLimit; i-- {
		window = append(window, messages[i])
	}

	for i, j := 0, len(window)-1; i < j; i, j = i+1, j-1 {
		window[i], window[j] = window[j], window[i]
	}

	return e.resumeMsgBuilder(toProviderType, window)
}

func shouldAttachResumeMessagesForProviderSwitch(fromProviderType, toProviderType string) bool {
	to := strings.TrimSpace(toProviderType)
	from := strings.TrimSpace(fromProviderType)
	if from == "" || to == "" {
		return false
	}
	return from != to
}
