package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	tygo "github.com/gzuidhof/tygo/tygo"
)

func main() {
	repoRoot, err := findRepoRoot()
	if err != nil {
		panic(err)
	}

	generatedDir := filepath.Join(repoRoot, "frontend", "src", "types", "generated")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		panic(err)
	}
	realtimePath := filepath.Join(generatedDir, "realtime.ts")
	transcriptPath := filepath.Join(generatedDir, "transcript.ts")

	gen := tygo.New(&tygo.Config{
		TypeMappings: map[string]string{
			"time.Time": "string",
		},
		Packages: []*tygo.PackageConfig{
			{
				Path:             "github.com/ricochet1k/orbitmesh/pkg/realtime",
				OutputPath:       realtimePath,
				PreserveComments: "none",
			},
			{
				Path:             "github.com/ricochet1k/orbitmesh/pkg/transcript",
				OutputPath:       transcriptPath,
				PreserveComments: "none",
			},
		},
	})

	if err := gen.Generate(); err != nil {
		panic(err)
	}

	fmt.Printf("wrote %s\n", realtimePath)
	fmt.Printf("wrote %s\n", transcriptPath)
}

func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to resolve generator path")
	}
	backendDir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	repoRoot := filepath.Clean(filepath.Join(backendDir, ".."))
	if _, err := os.Stat(filepath.Join(backendDir, "go.mod")); err != nil {
		return "", fmt.Errorf("backend module not found: %w", err)
	}
	return repoRoot, nil
}
