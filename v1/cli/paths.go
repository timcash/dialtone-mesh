package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func locateMeshV3Root() (string, error) {
	if envRoot := os.Getenv("DIALTONE_REPO_ROOT"); envRoot != "" {
		candidate := filepath.Join(envRoot, "src", "mods", "mesh", "v3")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(cwd, "src", "mods", "mesh", "v3")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("unable to locate mesh v3 root from %s", cwd)
		}
		cwd = parent
	}
}
