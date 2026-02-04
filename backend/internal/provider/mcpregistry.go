package provider

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// MCPRegistry provides basic validation for MCP server configurations.
//
// TRUST MODEL:
// MCP configs are trusted. Users run OrbitMesh on their own hardware and
// provide their own MCP servers. This registry exists for basic validation
// (misconfiguration detection, resource limits), NOT security sandboxing.
//
// SHELL INTERPRETERS ARE ALLOWED:
// Commands like "python", "node", "bash" are NOT blocked because MCP servers
// are commonly implemented as Python/Node scripts. Non-MCP processes will fail
// the MCP protocol handshake anyway.
//
// VALIDATION (with allowAll=true, the default):
// - Command must be an absolute path (catches misconfigurations)
// - Arguments cannot contain NUL bytes (prevents OS-level issues)
// - Argument count/length limits (prevents resource exhaustion)
//
// REGISTRY MODES:
// - allowAll=true (default): Any absolute path allowed with basic validation
// - allowAll=false: Requires explicit registration of allowed servers (stricter)

var (
	ErrMCPNotRegistered     = errors.New("MCP server not registered in allowlist")
	ErrMCPCommandNotAllowed = errors.New("MCP command not allowed")
	ErrMCPInvalidPath       = errors.New("MCP command path is not absolute")
	ErrMCPArgsTooMany       = errors.New("MCP command has too many arguments")
	ErrMCPArgTooLong        = errors.New("MCP argument exceeds maximum length")
	ErrMCPInvalidArg        = errors.New("MCP argument contains invalid characters")
	ErrMCPRegistryDisabled  = errors.New("MCP subprocess execution is disabled")
)

const (
	DefaultMaxArgs      = 50
	DefaultMaxArgLength = 4096
)

type MCPRegistryEntry struct {
	Name           string
	Command        string
	AllowedArgs    []string
	AllowAnyArgs   bool
	MaxArgs        int
	MaxArgLength   int
}

type MCPRegistry struct {
	mu       sync.RWMutex
	entries  map[string]MCPRegistryEntry
	enabled  bool
	allowAll bool
}

func NewMCPRegistry() *MCPRegistry {
	return &MCPRegistry{
		entries: make(map[string]MCPRegistryEntry),
		enabled: false,
	}
}

func (r *MCPRegistry) Enable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = true
}

func (r *MCPRegistry) Disable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = false
}

func (r *MCPRegistry) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled
}

func (r *MCPRegistry) SetAllowAll(allow bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowAll = allow
}

func (r *MCPRegistry) Register(entry MCPRegistryEntry) error {
	if !filepath.IsAbs(entry.Command) {
		return fmt.Errorf("%w: %s", ErrMCPInvalidPath, entry.Command)
	}

	if entry.MaxArgs == 0 {
		entry.MaxArgs = DefaultMaxArgs
	}
	if entry.MaxArgLength == 0 {
		entry.MaxArgLength = DefaultMaxArgLength
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[entry.Name] = entry
	return nil
}

func (r *MCPRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

func (r *MCPRegistry) Get(name string) (MCPRegistryEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[name]
	return entry, ok
}

func (r *MCPRegistry) List() []MCPRegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]MCPRegistryEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry)
	}
	return result
}

func (r *MCPRegistry) Validate(cfg MCPServerConfig) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.enabled {
		return ErrMCPRegistryDisabled
	}

	if r.allowAll {
		return r.validateCommand(cfg.Command, cfg.Args, DefaultMaxArgs, DefaultMaxArgLength)
	}

	entry, ok := r.entries[cfg.Name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMCPNotRegistered, cfg.Name)
	}

	if cfg.Command != entry.Command {
		return fmt.Errorf("%w: expected %s, got %s", ErrMCPCommandNotAllowed, entry.Command, cfg.Command)
	}

	return r.validateArgs(cfg.Args, entry)
}

func (r *MCPRegistry) validateCommand(command string, args []string, maxArgs, maxArgLength int) error {
	if !filepath.IsAbs(command) {
		return fmt.Errorf("%w: %s", ErrMCPInvalidPath, command)
	}

	if len(args) > maxArgs {
		return fmt.Errorf("%w: got %d, max %d", ErrMCPArgsTooMany, len(args), maxArgs)
	}

	for _, arg := range args {
		if len(arg) > maxArgLength {
			return fmt.Errorf("%w: length %d exceeds max %d", ErrMCPArgTooLong, len(arg), maxArgLength)
		}
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%w: contains NUL byte", ErrMCPInvalidArg)
		}
	}

	return nil
}

func (r *MCPRegistry) validateArgs(args []string, entry MCPRegistryEntry) error {
	if len(args) > entry.MaxArgs {
		return fmt.Errorf("%w: got %d, max %d", ErrMCPArgsTooMany, len(args), entry.MaxArgs)
	}

	for _, arg := range args {
		if len(arg) > entry.MaxArgLength {
			return fmt.Errorf("%w: length %d exceeds max %d", ErrMCPArgTooLong, len(arg), entry.MaxArgLength)
		}
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%w: contains NUL byte", ErrMCPInvalidArg)
		}
	}

	if entry.AllowAnyArgs {
		return nil
	}

	if len(entry.AllowedArgs) > 0 {
		allowedSet := make(map[string]bool, len(entry.AllowedArgs))
		for _, allowed := range entry.AllowedArgs {
			allowedSet[allowed] = true
		}
		for _, arg := range args {
			if !allowedSet[arg] {
				return fmt.Errorf("%w: argument %q not in allowlist", ErrMCPCommandNotAllowed, arg)
			}
		}
	}

	return nil
}

var globalMCPRegistry = newDefaultMCPRegistry()

func newDefaultMCPRegistry() *MCPRegistry {
	r := NewMCPRegistry()
	r.Enable()
	r.SetAllowAll(true)
	return r
}

func GlobalMCPRegistry() *MCPRegistry {
	return globalMCPRegistry
}
