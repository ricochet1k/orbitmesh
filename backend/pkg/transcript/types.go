package transcript

type TranscriptPayload interface {
	isTranscriptPayload()
}

type OutputThoughtPayload struct {
	Content   string `json:"content"`
	IsDelta   bool   `json:"is_delta"`
	MessageID string `json:"message_id"`
}

func (OutputThoughtPayload) isTranscriptPayload() {}

type ToolCallPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Input     any    `json:"input"`
	Output    any    `json:"output"`
	Arguments string `json:"arguments,omitempty"`
}

func (ToolCallPayload) isTranscriptPayload() {}

type ProgressPayload struct {
	Channel  string `json:"channel"`
	StreamID string `json:"stream_id"`
	Content  string `json:"content"`
	IsDelta  bool   `json:"is_delta"`
	Done     bool   `json:"done"`
	Status   string `json:"status"`
}

func (ProgressPayload) isTranscriptPayload() {}

type ActionRequestPayload struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Payload any    `json:"payload"`
}

func (ActionRequestPayload) isTranscriptPayload() {}

type ArtifactUpdatePayload struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	IsDelta bool   `json:"is_delta"`
	Payload any    `json:"payload"`
}

func (ArtifactUpdatePayload) isTranscriptPayload() {}
