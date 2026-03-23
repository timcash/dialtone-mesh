package meshv3_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMeshV3LayoutAndCargoDeps(t *testing.T) {
	root := currentDir(t)
	for _, rel := range []string{
		"README.md",
		"Cargo.toml",
		filepath.Join("src", "main.rs"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected %s in mesh/v3: %v", rel, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		t.Fatalf("read Cargo.toml: %v", err)
	}
	text := string(content)
	for _, dep := range []string{"iroh", "iroh-gossip", "iroh-ping", "iroh-tickets"} {
		if !strings.Contains(text, dep) {
			t.Fatalf("expected Cargo.toml to mention %q", dep)
		}
	}
}

func currentDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}
