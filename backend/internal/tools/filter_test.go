package tools_test

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/tools"
)

// makeDefs is a helper that builds a []ToolDef from a list of names.
func makeDefs(names ...string) []tools.ToolDef {
	out := make([]tools.ToolDef, len(names))
	for i, n := range names {
		out[i] = tools.ToolDef{Name: n}
	}
	return out
}

// defNames extracts the Name field from each ToolDef.
func defNames(defs []tools.ToolDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func TestFilter_EmptyListsReturnAll(t *testing.T) {
	input := makeDefs("a", "b", "c")
	got := tools.Filter(input, nil, nil)
	if len(got) != len(input) {
		t.Fatalf("Filter(_, nil, nil): got %d tools, want %d", len(got), len(input))
	}
}

func TestFilter_AllowlistRestricts(t *testing.T) {
	input := makeDefs("a", "b", "c", "d")
	got := tools.Filter(input, []string{"b", "d"}, nil)
	names := defNames(got)
	want := []string{"b", "d"}
	if len(names) != len(want) {
		t.Fatalf("Filter allowlist: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Filter allowlist[%d]: got %q, want %q", i, names[i], want[i])
		}
	}
}

func TestFilter_DenylistRemoves(t *testing.T) {
	input := makeDefs("a", "b", "c")
	got := tools.Filter(input, nil, []string{"b"})
	names := defNames(got)
	want := []string{"a", "c"}
	if len(names) != len(want) {
		t.Fatalf("Filter denylist: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Filter denylist[%d]: got %q, want %q", i, names[i], want[i])
		}
	}
}

func TestFilter_AllowAndDenyTogether(t *testing.T) {
	// Allow {b, c, d}, then deny {c}.  Result: {b, d}.
	input := makeDefs("a", "b", "c", "d", "e")
	got := tools.Filter(input, []string{"b", "c", "d"}, []string{"c"})
	names := defNames(got)
	want := []string{"b", "d"}
	if len(names) != len(want) {
		t.Fatalf("Filter allow+deny: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Filter allow+deny[%d]: got %q, want %q", i, names[i], want[i])
		}
	}
}

func TestFilter_AllowlistNotInInput(t *testing.T) {
	// Allowlist references a tool not present in input — result should be empty.
	input := makeDefs("a", "b")
	got := tools.Filter(input, []string{"z"}, nil)
	if len(got) != 0 {
		t.Errorf("Filter allowlist-not-in-input: got %v, want empty", defNames(got))
	}
}

func TestFilter_DenyAllViaAllowlist(t *testing.T) {
	// Empty allowlist (non-nil but zero length) means "no restriction", not "deny all".
	input := makeDefs("a", "b")
	got := tools.Filter(input, []string{}, nil)
	if len(got) != len(input) {
		t.Errorf("Filter empty-allowlist: got %d tools, want %d (no restriction)", len(got), len(input))
	}
}

func TestFilter_DenylistSupersedesAllow(t *testing.T) {
	// Even if a tool is in the allowlist, the denylist removes it.
	input := makeDefs("a", "b")
	got := tools.Filter(input, []string{"a", "b"}, []string{"a"})
	names := defNames(got)
	want := []string{"b"}
	if len(names) != len(want) || names[0] != want[0] {
		t.Errorf("Filter deny-supersedes-allow: got %v, want %v", names, want)
	}
}

func TestFilter_EmptyInput(t *testing.T) {
	got := tools.Filter(nil, []string{"a"}, nil)
	if len(got) != 0 {
		t.Errorf("Filter nil input: got %d tools, want 0", len(got))
	}
}
