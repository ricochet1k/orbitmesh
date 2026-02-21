package realtime

const TopicSessionsState = "sessions.state"

func IsSupportedTopic(topic string) bool {
	switch topic {
	case TopicSessionsState:
		return true
	default:
		return false
	}
}
