package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/tools"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := tools.NewRegistry()

	def := tools.ToolDef{
		Name:        "my_tool",
		Description: "does something",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	if err := r.Register(def); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, ok := r.Lookup("my_tool")
	if !ok {
		t.Fatal("Lookup: tool not found after registration")
	}
	if got.Name != def.Name {
		t.Errorf("Lookup: Name = %q, want %q", got.Name, def.Name)
	}
	if got.Description != def.Description {
		t.Errorf("Lookup: Description = %q, want %q", got.Description, def.Description)
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	r := tools.NewRegistry()
	_, ok := r.Lookup("nonexistent")
	if ok {
		t.Error("Lookup: expected false for missing tool, got true")
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := tools.NewRegistry()

	def := tools.ToolDef{Name: "dup_tool"}
	if err := r.Register(def); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}
	if err := r.Register(def); err == nil {
		t.Error("second Register: expected error for duplicate name, got nil")
	}
}

func TestRegistry_ListReturnsAll(t *testing.T) {
	r := tools.NewRegistry()

	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		if err := r.Register(tools.ToolDef{Name: name}); err != nil {
			t.Fatalf("Register %q: %v", name, err)
		}
	}

	list := r.List()
	if len(list) != len(names) {
		t.Fatalf("List: got %d tools, want %d", len(list), len(names))
	}

	// Verify insertion order is preserved.
	for i, def := range list {
		if def.Name != names[i] {
			t.Errorf("List[%d]: Name = %q, want %q", i, def.Name, names[i])
		}
	}
}

func TestRegistry_ListEmpty(t *testing.T) {
	r := tools.NewRegistry()
	list := r.List()
	if len(list) != 0 {
		t.Errorf("List on empty registry: got %d tools, want 0", len(list))
	}
}

func TestGlobal_Singleton(t *testing.T) {
	a := tools.Global()
	b := tools.Global()
	if a != b {
		t.Error("Global: expected the same instance on repeated calls")
	}
}

func TestGlobal_RegisterVisible(t *testing.T) {
	// Registering via Global() is visible in subsequent calls.
	g := tools.Global()

	// Use a unique name to avoid collisions with other tests sharing the global.
	const uniqueName = "_test_global_visibility_tool"
	_ = g.Register(tools.ToolDef{Name: uniqueName}) // ignore duplicate error if tests run multiple times

	_, ok := tools.Global().Lookup(uniqueName)
	if !ok {
		t.Errorf("Global: tool %q registered but not visible via Global()", uniqueName)
	}
}
