package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMeshV1CLISmoke(t *testing.T) {
	for _, name := range []string{
		"main.go",
		"install.go",
		"build.go",
		"paths.go",
	} {
		path := filepath.Join(testDataDir(), name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("mesh v1 cli is missing %s: %v", name, err)
		}
	}

	usage := captureStdout(t, printUsage)
	for _, cmd := range []string{
		"install",
		"build",
	} {
		if !strings.Contains(usage, cmd) {
			t.Fatalf("mesh v1 usage missing command %q", cmd)
		}
	}

	build := parseBuildArgs([]string{})
	if build.target != "native" || build.rebuild {
		t.Fatalf("parseBuildArgs defaults unexpected: %#v", build)
	}

	build = parseBuildArgs([]string{"--target", "rover", "--rebuild"})
	if build.target != "rover" || !build.rebuild {
		t.Fatalf("parseBuildArgs failed to parse explicit flags: %#v", build)
	}

	if _, err := locateMeshV3Root(); err != nil {
		t.Fatalf("locateMeshV3Root should resolve from mod source tree")
	}

	attr, out := meshBuildAttrAndOutLink("native")
	if attr != ".#mesh-v3" || out != ".result-native" {
		t.Fatalf("meshBuildAttrAndOutLink returned unexpected values: %s %s", attr, out)
	}
	attr, out = meshBuildAttrAndOutLink("rover")
	if attr != ".#mesh-v3-rover" || out != ".result-rover" {
		t.Fatalf("meshBuildAttrAndOutLink(rover) returned unexpected values: %s %s", attr, out)
	}
}

func testDataDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("unable to locate test source file")
	}
	return filepath.Dir(file)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("reading stdout pipe failed: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader failed: %v", err)
	}
	return buf.String()
}
