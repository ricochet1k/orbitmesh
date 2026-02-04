package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestNewMCPRegistry(t *testing.T) {
	r := NewMCPRegistry()
	if r == nil {
		t.Fatal("expected registry to be created")
	}
	if r.IsEnabled() {
		t.Error("registry should be disabled by default")
	}
}

func TestMCPRegistry_EnableDisable(t *testing.T) {
	r := NewMCPRegistry()

	r.Enable()
	if !r.IsEnabled() {
		t.Error("registry should be enabled after Enable()")
	}

	r.Disable()
	if r.IsEnabled() {
		t.Error("registry should be disabled after Disable()")
	}
}

func TestMCPRegistry_Register(t *testing.T) {
	r := NewMCPRegistry()

	err := r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := r.Get("test-server")
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if entry.Command != "/usr/bin/test-mcp" {
		t.Errorf("expected command '/usr/bin/test-mcp', got %s", entry.Command)
	}
}

func TestMCPRegistry_RegisterRelativePath(t *testing.T) {
	r := NewMCPRegistry()

	err := r.Register(MCPRegistryEntry{
		Name:    "bad-server",
		Command: "test-mcp",
	})
	if !errors.Is(err, ErrMCPInvalidPath) {
		t.Errorf("expected ErrMCPInvalidPath, got %v", err)
	}
}

func TestMCPRegistry_Unregister(t *testing.T) {
	r := NewMCPRegistry()

	_ = r.Register(MCPRegistryEntry{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
	})

	r.Unregister("test-server")

	_, ok := r.Get("test-server")
	if ok {
		t.Error("expected entry to be removed")
	}
}

func TestMCPRegistry_List(t *testing.T) {
	r := NewMCPRegistry()

	_ = r.Register(MCPRegistryEntry{
		Name:    "server1",
		Command: "/usr/bin/server1",
	})
	_ = r.Register(MCPRegistryEntry{
		Name:    "server2",
		Command: "/usr/bin/server2",
	})

	list := r.List()
	if len(list) != 2 {
		t.Errorf("expected 2 entries, got %d", len(list))
	}
}

func TestMCPRegistry_ValidateDisabled(t *testing.T) {
	r := NewMCPRegistry()

	err := r.Validate(MCPServerConfig{
		Name:    "test",
		Command: "/usr/bin/test",
	})
	if !errors.Is(err, ErrMCPRegistryDisabled) {
		t.Errorf("expected ErrMCPRegistryDisabled, got %v", err)
	}
}

func TestMCPRegistry_ValidateNotRegistered(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	err := r.Validate(MCPServerConfig{
		Name:    "unknown",
		Command: "/usr/bin/unknown",
	})
	if !errors.Is(err, ErrMCPNotRegistered) {
		t.Errorf("expected ErrMCPNotRegistered, got %v", err)
	}
}

func TestMCPRegistry_ValidateWrongCommand(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:    "test-server",
		Command: "/usr/bin/legit-mcp",
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/evil-binary",
	})
	if !errors.Is(err, ErrMCPCommandNotAllowed) {
		t.Errorf("expected ErrMCPCommandNotAllowed, got %v", err)
	}
}

func TestMCPRegistry_ValidateSuccess(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"--port", "8080"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMCPRegistry_ValidateAllowedArgs(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:        "test-server",
		Command:     "/usr/bin/test-mcp",
		AllowedArgs: []string{"--safe", "--approved"},
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"--safe"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"--evil"},
	})
	if !errors.Is(err, ErrMCPCommandNotAllowed) {
		t.Errorf("expected ErrMCPCommandNotAllowed for unapproved arg, got %v", err)
	}
}

func TestMCPRegistry_ValidateTooManyArgs(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
		MaxArgs:      5,
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"1", "2", "3", "4", "5", "6"},
	})
	if !errors.Is(err, ErrMCPArgsTooMany) {
		t.Errorf("expected ErrMCPArgsTooMany, got %v", err)
	}
}

func TestMCPRegistry_ValidateArgTooLong(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
		MaxArgLength: 10,
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"this-is-too-long"},
	})
	if !errors.Is(err, ErrMCPArgTooLong) {
		t.Errorf("expected ErrMCPArgTooLong, got %v", err)
	}
}

func TestMCPRegistry_ValidateNulByte(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{"arg\x00with-null"},
	})
	if !errors.Is(err, ErrMCPInvalidArg) {
		t.Errorf("expected ErrMCPInvalidArg for NUL byte, got %v", err)
	}
}

func TestMCPRegistry_AllowAll(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()
	r.SetAllowAll(true)

	err := r.Validate(MCPServerConfig{
		Name:    "any-server",
		Command: "/usr/bin/any-mcp",
		Args:    []string{"--any", "--args"},
	})
	if err != nil {
		t.Errorf("unexpected error with allowAll: %v", err)
	}
}

func TestMCPRegistry_AllowAllStillValidatesPath(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()
	r.SetAllowAll(true)

	err := r.Validate(MCPServerConfig{
		Name:    "relative",
		Command: "relative-path",
	})
	if !errors.Is(err, ErrMCPInvalidPath) {
		t.Errorf("expected ErrMCPInvalidPath even with allowAll, got %v", err)
	}
}

func TestMCPRegistry_DefaultLimits(t *testing.T) {
	r := NewMCPRegistry()

	_ = r.Register(MCPRegistryEntry{
		Name:         "test-server",
		Command:      "/usr/bin/test-mcp",
		AllowAnyArgs: true,
	})

	entry, _ := r.Get("test-server")
	if entry.MaxArgs != DefaultMaxArgs {
		t.Errorf("expected default max args %d, got %d", DefaultMaxArgs, entry.MaxArgs)
	}
	if entry.MaxArgLength != DefaultMaxArgLength {
		t.Errorf("expected default max arg length %d, got %d", DefaultMaxArgLength, entry.MaxArgLength)
	}
}

func TestGlobalMCPRegistry(t *testing.T) {
	r := GlobalMCPRegistry()
	if r == nil {
		t.Fatal("expected global registry to exist")
	}

	r2 := GlobalMCPRegistry()
	if r != r2 {
		t.Error("expected same instance from GlobalMCPRegistry()")
	}
}

func TestMCPRegistry_ConcurrentAccess(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = r.Register(MCPRegistryEntry{
				Name:    "server",
				Command: "/usr/bin/server",
			})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_, _ = r.Get("server")
		_ = r.List()
		_ = r.IsEnabled()
	}

	<-done
}

func TestMCPRegistry_ValidateEmptyArgs(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()

	_ = r.Register(MCPRegistryEntry{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
	})

	err := r.Validate(MCPServerConfig{
		Name:    "test-server",
		Command: "/usr/bin/test-mcp",
		Args:    []string{},
	})
	if err != nil {
		t.Errorf("empty args should be valid: %v", err)
	}
}

func TestMCPRegistry_ValidateLargeArg(t *testing.T) {
	r := NewMCPRegistry()
	r.Enable()
	r.SetAllowAll(true)

	largeArg := strings.Repeat("x", DefaultMaxArgLength+1)

	err := r.Validate(MCPServerConfig{
		Name:    "test",
		Command: "/usr/bin/test-mcp",
		Args:    []string{largeArg},
	})
	if !errors.Is(err, ErrMCPArgTooLong) {
		t.Errorf("expected ErrMCPArgTooLong for large arg, got %v", err)
	}
}
