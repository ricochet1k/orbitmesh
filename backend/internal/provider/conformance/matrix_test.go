package conformance

import (
	"testing"

	"github.com/ricochet1k/orbitmesh/internal/storage"
)

func TestBuildProviderMatrix_SkipsPTYByDefault(t *testing.T) {
	matrix, err := BuildProviderMatrix([]string{"adk", "pty", "openai"}, nil, nil)
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 2 {
		t.Fatalf("matrix size = %d, want 2", len(matrix))
	}
	if matrix[0].ProviderType != "adk" || matrix[1].ProviderType != "openai" {
		t.Fatalf("matrix providers = %v, want [adk openai]", []string{matrix[0].ProviderType, matrix[1].ProviderType})
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

func TestBuildProviderMatrix_ExplicitPTYStillExcluded(t *testing.T) {
	matrix, err := BuildProviderMatrix([]string{"pty", "openai"}, nil, []string{"pty", "openai"})
	if err != nil {
		t.Fatalf("BuildProviderMatrix() error = %v", err)
	}
	if len(matrix) != 1 || matrix[0].ProviderType != "openai" {
		t.Fatalf("matrix = %+v, want only openai", matrix)
	}
}
