package api

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

func resolveGitDir() string {
	if dir := os.Getenv("ORBITMESH_GIT_DIR"); dir != "" {
		return dir
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		root := strings.TrimSpace(out.String())
		if root != "" {
			return root
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}

	return "."
}
