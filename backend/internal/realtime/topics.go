package realtime

const TopicSessionsState = "sessions.state"
const TopicTerminalsState = "terminals.state"

const sessionsActivityPrefix = "sessions.activity:"
const terminalsOutputPrefix = "terminals.output:"

func IsSupportedTopic(topic string) bool {
	switch topic {
	case TopicSessionsState:
		return true
	case TopicTerminalsState:
		return true
	default:
		if _, ok := SessionIDFromActivityTopic(topic); ok {
			return true
		}
		if _, ok := TerminalIDFromOutputTopic(topic); ok {
			return true
		}
		return false
	}
}

func TopicSessionsActivity(sessionID string) string {
	return sessionsActivityPrefix + sessionID
}

func TopicTerminalsOutput(terminalID string) string {
	return terminalsOutputPrefix + terminalID
}

func SessionIDFromActivityTopic(topic string) (string, bool) {
	if len(topic) <= len(sessionsActivityPrefix) || topic[:len(sessionsActivityPrefix)] != sessionsActivityPrefix {
		return "", false
	}
	sessionID := topic[len(sessionsActivityPrefix):]
	if sessionID == "" {
		return "", false
	}
	return sessionID, true
}

func TerminalIDFromOutputTopic(topic string) (string, bool) {
	if len(topic) <= len(terminalsOutputPrefix) || topic[:len(terminalsOutputPrefix)] != terminalsOutputPrefix {
		return "", false
	}
	terminalID := topic[len(terminalsOutputPrefix):]
	if terminalID == "" {
		return "", false
	}
	return terminalID, true
}
