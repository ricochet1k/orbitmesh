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

	outputPath := filepath.Join(repoRoot, "frontend", "src", "types", "generated", "realtime.ts")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		panic(err)
	}

	gen := tygo.New(&tygo.Config{
		TypeMappings: map[string]string{
			"time.Time": "string",
		},
		Packages: []*tygo.PackageConfig{
			{
				Path:             "github.com/ricochet1k/orbitmesh/pkg/realtime",
				OutputPath:       outputPath,
				PreserveComments: "none",
			},
		},
	})

	if err := gen.Generate(); err != nil {
		panic(err)
	}

	fmt.Printf("wrote %s\n", outputPath)
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
