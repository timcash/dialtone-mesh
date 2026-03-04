package main

import (
	"fmt"
	"os"
)

func printUsage() {
	fmt.Println("mesh cli usage: mesh <install|build> [args]")
	fmt.Println("  install")
	fmt.Println("    Ensure required nix development dependencies are available")
	fmt.Println("  build [--rebuild] [--target native|rover]")
	fmt.Println("    Build mesh-v3 binary via nix and link to <repo-root>/bin")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}
	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "-h", "--help", "help":
		printUsage()
		return
	}

	switch command {
	case "install":
		if len(args) > 0 {
			printUsage()
			os.Exit(1)
		}
		if err := runNixInstall(); err != nil {
			fmt.Fprintln(os.Stderr, "mesh install error:", err)
			os.Exit(1)
		}
	case "build":
		if err := runNixBuild(parseBuildArgs(args)); err != nil {
			fmt.Fprintln(os.Stderr, "mesh build error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mesh cli command: %s\n", command)
		os.Exit(1)
	}
}
