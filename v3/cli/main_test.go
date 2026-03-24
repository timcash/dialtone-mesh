package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseBuildArgs(t *testing.T) {
	opts, err := parseBuildArgs([]string{"--target", "rover", "--rebuild"})
	if err != nil {
		t.Fatalf("parseBuildArgs returned error: %v", err)
	}
	if opts.target != "rover" || !opts.rebuild {
		t.Fatalf("unexpected build options: %+v", opts)
	}
}

func TestMeshCLIUsageIncludesContractCommands(t *testing.T) {
	output := captureStdout(t, printUsage)
	for _, want := range []string{"install", "build", "format", "test", "lint"} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage missing %q: %s", want, output)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	done := make(chan string, 1)
	go func() {
		var buf [4096]byte
		var out strings.Builder
		for {
			n, readErr := r.Read(buf[:])
			if n > 0 {
				out.Write(buf[:n])
			}
			if readErr != nil {
				done <- out.String()
				return
			}
		}
	}()
	fn()
	_ = w.Close()
	return <-done
}
