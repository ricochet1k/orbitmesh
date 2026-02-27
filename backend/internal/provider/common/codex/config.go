package codex

// Config defines static Codex app-server provider settings.
// Session-level values from session.Config.Custom can override these.
type Config struct {
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}
