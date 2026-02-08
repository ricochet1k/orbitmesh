package pty

func NewClaudePTYProvider(sessionID string) *PTYProvider {
	return NewPTYProvider(sessionID)
}
