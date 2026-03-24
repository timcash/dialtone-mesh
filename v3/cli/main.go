package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"dialtone/dev/internal/modcli"
)

type buildOptions struct {
	target  string
	rebuild bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "help", "-h", "--help":
		printUsage()
	case "install":
		exitIfErr(runInstall(args), "mesh install")
	case "build":
		exitIfErr(runBuild(args), "mesh build")
	case "format", "fmt":
		exitIfErr(runFormat(args), "mesh format")
	case "lint":
		exitIfErr(runLint(args), "mesh lint")
	case "test":
		exitIfErr(runTest(args), "mesh test")
	case "logs":
		exitIfErr(runLogs(args), "mesh logs")
	default:
		exitIfErr(runBinary(append([]string{command}, args...)), "mesh runtime")
	}
}

func runInstall(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("mesh install does not accept positional arguments")
	}
	cmd, err := meshNixCommand("develop", ".", "--command", "cargo", "--version")
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh install failed: %w", err)
	}
	fmt.Println("mesh v3 install complete")
	return nil
}

func runBuild(args []string) error {
	opts, err := parseBuildArgs(args)
	if err != nil {
		return err
	}
	repoRoot, err := modcli.FindRepoRoot()
	if err != nil {
		return err
	}
	meshRoot := modcli.ModDir(repoRoot, "mesh", "v3")
	artifact := ".#mesh-v3"
	outLink := ".result-native"
	outputName := "mesh-v3"
	if opts.target == "rover" {
		artifact = ".#mesh-v3-rover"
		outLink = ".result-rover"
		outputName = "mesh-v3_rover"
	}
	if opts.rebuild {
		_ = os.Remove(filepath.Join(meshRoot, outLink))
	}
	cmd, err := meshNixCommand("build", artifact, "--out-link", outLink)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh build failed: %w", err)
	}
	source := filepath.Join(meshRoot, outLink, "bin", "mesh-v3")
	outputPath, err := modcli.BuildOutputPath(repoRoot, "mesh", "v3", outputName)
	if err != nil {
		return err
	}
	_ = os.Remove(outputPath)
	if err := os.Symlink(source, outputPath); err != nil {
		return fmt.Errorf("link mesh binary: %w", err)
	}
	fmt.Printf("built mesh v3 binary: %s\n", outputPath)
	return nil
}

func runFormat(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("mesh format does not accept positional arguments")
	}
	cmd, err := meshNixCommand("develop", ".", "--command", "cargo", "fmt")
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh format failed: %w", err)
	}
	return nil
}

func runLint(args []string) error {
	cmdArgs := []string{"develop", ".", "--command", "cargo", "clippy", "--all-targets", "--all-features", "--", "-D", "warnings"}
	cmd, err := meshNixCommand(cmdArgs...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh lint failed: %w", err)
	}
	return nil
}

func runTest(args []string) error {
	cmdArgs := []string{"develop", ".", "--command", "cargo", "test"}
	cmdArgs = append(cmdArgs, args...)
	cmd, err := meshNixCommand(cmdArgs...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh test failed: %w", err)
	}
	return nil
}

func runLogs(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("mesh logs does not accept positional arguments")
	}
	fmt.Println("mesh-v3 runtime logging is handled by the calling process or service")
	fmt.Println("Hint: capture logs with your process supervisor")
	return nil
}

func runBinary(args []string) error {
	repoRoot, err := modcli.FindRepoRoot()
	if err != nil {
		return err
	}
	binary := filepath.Join(repoRoot, "bin", "mods", "mesh", "v3", "mesh-v3")
	if _, err := os.Stat(binary); err != nil {
		if buildErr := runBuild(nil); buildErr != nil {
			return buildErr
		}
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func parseBuildArgs(argv []string) (buildOptions, error) {
	fs := flag.NewFlagSet("mesh v3 build", flag.ContinueOnError)
	target := fs.String("target", "native", "native or rover")
	rebuild := fs.Bool("rebuild", false, "Force a fresh nix out-link")
	if err := fs.Parse(argv); err != nil {
		return buildOptions{}, err
	}
	if fs.NArg() != 0 {
		return buildOptions{}, fmt.Errorf("mesh build does not accept positional arguments")
	}
	switch *target {
	case "native", "rover":
	default:
		return buildOptions{}, fmt.Errorf("unsupported --target %q", *target)
	}
	return buildOptions{target: *target, rebuild: *rebuild}, nil
}

func meshNixCommand(args ...string) (*exec.Cmd, error) {
	repoRoot, err := modcli.FindRepoRoot()
	if err != nil {
		return nil, err
	}
	meshRoot := modcli.ModDir(repoRoot, "mesh", "v3")
	command := exec.Command("nix", append([]string{"--extra-experimental-features", "nix-command flakes"}, args...)...)
	command.Dir = meshRoot
	return command, nil
}

func printUsage() {
	fmt.Println("Usage: ./dialtone_mod mesh v3 <command> [args]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  install")
	fmt.Println("       Verify cargo is available in the mesh v3 flake shell")
	fmt.Println("  build [--target native|rover] [--rebuild]")
	fmt.Println("       Build mesh-v3 and link it under <repo-root>/bin/mods/mesh/v3/")
	fmt.Println("  format")
	fmt.Println("       Run cargo fmt in mesh v3")
	fmt.Println("  lint")
	fmt.Println("       Run cargo clippy for mesh v3")
	fmt.Println("  test")
	fmt.Println("       Run cargo test for mesh v3")
	fmt.Println("  logs")
	fmt.Println("       Print logging guidance for mesh v3")
	fmt.Println("  node|index|hub|connect|register|list ...")
	fmt.Println("       Run the built mesh-v3 binary, building the native artifact first if needed")
}

func exitIfErr(err error, context string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s error: %v\n", context, err)
	os.Exit(1)
}
