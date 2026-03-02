package conformance

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestBuildProviderMatrix_IncludesIgnoredProviders(t *testing.T) {
	matrix, err := BuildProviderMatrix([]string{"adk", "pty", "openai"}, nil, nil)
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 3 {
		t.Fatalf("matrix size = %d, want 3", len(matrix))
	}
	if matrix[0].ProviderType != "adk" || matrix[1].ProviderType != "openai" || matrix[2].ProviderType != "pty" {
		t.Fatalf("matrix providers = %v, want [adk openai pty]", []string{matrix[0].ProviderType, matrix[1].ProviderType, matrix[2].ProviderType})
	}
	if !matrix[0].Capabilities.Ignored || !matrix[2].Capabilities.Ignored {
		t.Fatalf("expected adk and pty to be marked ignored, got %+v %+v", matrix[0].Capabilities, matrix[2].Capabilities)
	}
}

func TestBuildProviderMatrix_FiltersSelectedProviders(t *testing.T) {
	configs := []storage.ProviderConfig{
		{ID: "cfg1", Type: "openai", IsActive: false},
		{ID: "cfg2", Type: "openai", IsActive: true},
	}
	matrix, err := BuildProviderMatrix([]string{"adk", "openai"}, configs, []string{"openai"})
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 1 {
		t.Fatalf("matrix size = %d, want 1", len(matrix))
	}
	if matrix[0].ProviderType != "openai" {
		t.Fatalf("provider type = %s, want openai", matrix[0].ProviderType)
	}
	if matrix[0].Config == nil || matrix[0].Config.ID != "cfg2" {
		t.Fatalf("config id = %v, want cfg2", matrix[0].Config)
	}
}

func TestBuildProviderMatrix_UnknownProviderReturnsError(t *testing.T) {
	_, err := BuildProviderMatrix([]string{"adk"}, nil, []string{"openai"})
	if err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestBuildProviderMatrix_ExplicitPTYIsIncludedButIgnored(t *testing.T) {
	matrix, err := BuildProviderMatrix([]string{"pty", "openai"}, nil, []string{"pty", "openai"})
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 2 {
		t.Fatalf("matrix len = %d, want 2", len(matrix))
	}
	if matrix[0].ProviderType != "openai" || matrix[1].ProviderType != "pty" {
		t.Fatalf("matrix providers = %+v, want openai + pty", matrix)
	}
	if !matrix[1].Capabilities.Ignored {
		t.Fatalf("expected pty to be ignored, got %+v", matrix[1].Capabilities)
	}
}
