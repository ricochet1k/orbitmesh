package acp

import "github.com/ricochet1k/orbitmesh/internal/session"

// Config defines configuration for an ACP provider instance.
type Config struct {
	// Command to execute the ACP agent (e.g., "gemini")
	Command string `json:"command"`

	// Args to pass to the command (e.g., ["--experimental-acp"])
	Args []string `json:"args"`

	// WorkingDir for the agent process
	WorkingDir string `json:"working_dir,omitempty"`

	// Environment variables to set for the agent process
	Environment map[string]string `json:"environment,omitempty"`
}

// SessionConfig extends the base session.Config with ACP-specific settings.
type SessionConfig struct {
	session.Config

	// ACPCommand overrides the default command if set
	ACPCommand string `json:"acp_command,omitempty"`

	// ACPArgs overrides the default args if set
	ACPArgs []string `json:"acp_args,omitempty"`
}
