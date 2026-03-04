package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type buildArgs struct {
	rebuild bool
	target  string
}

func runNixBuild(args buildArgs) error {
	meshRoot, err := locateMeshV3Root()
	if err != nil {
		return err
	}

	attr, outLink := meshBuildAttrAndOutLink(args.target)
	buildCmd := []string{
		"--extra-experimental-features", "nix-command flakes",
		"build",
		attr,
		"--out-link", outLink,
	}
	if args.rebuild {
		buildCmd = append(buildCmd, "--rebuild")
	}

	cmd := exec.Command("nix", buildCmd...)
	cmd.Dir = meshRoot
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nix build failed: %w", err)
	}
	return linkMeshBinary(meshRoot, args.target)
}

func parseBuildArgs(argv []string) buildArgs {
	result := buildArgs{
		rebuild: false,
		target:  "native",
	}
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--rebuild":
			result.rebuild = true
		case "--target":
			if i+1 < len(argv) {
				result.target = argv[i+1]
			}
		default:
			if (argv[i] == "native") || (argv[i] == "rover") {
				result.target = argv[i]
			}
		}
	}
	return result
}

func linkMeshBinary(meshRoot, target string) error {
	targetFile := "mesh-v3_" + goArchToBinary(runtime.GOARCH)
	if target == "rover" {
		targetFile = "mesh-v3_arm64"
	}
	binDir, err := resolveBinDir(meshRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	outLink := ".result-" + target
	binTarget := filepath.Join(binDir, targetFile)
	binSource := filepath.Join(meshRoot, outLink, "bin", "mesh-v3")

	_ = os.Remove(binTarget)
	if err := os.Symlink(binSource, binTarget); err != nil {
		return fmt.Errorf("mesh v3 binary link failed (%s -> %s): %w", binSource, binTarget, err)
	}
	fmt.Printf("Built (%s): %s -> %s\n", target, binTarget, binSource)
	return nil
}

func resolveBinDir(meshRoot string) (string, error) {
	if repoRoot := os.Getenv("DIALTONE_REPO_ROOT"); repoRoot != "" {
		return filepath.Join(repoRoot, "bin"), nil
	}
	repoRoot := filepath.Clean(filepath.Join(meshRoot, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "dialtone.sh")); err == nil {
		return filepath.Join(repoRoot, "bin"), nil
	}
	return "", fmt.Errorf("unable to resolve repository root for mesh outputs from %s", meshRoot)
}

func meshBuildAttrAndOutLink(target string) (string, string) {
	attrName := "mesh-v3"
	if target == "rover" {
		attrName = "mesh-v3-rover"
	}
	return ".#" + attrName, ".result-" + target
}

func goArchToBinary(goArch string) string {
	if goArch == "amd64" {
		return "x86_64"
	}
	return goArch
}
