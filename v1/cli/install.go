package main

import (
	"fmt"
	"os"
	"os/exec"
)

func runNixInstall() error {
	meshRoot, err := locateMeshV3Root()
	if err != nil {
		return err
	}
	buildArgs := []string{"--extra-experimental-features", "nix-command flakes", "develop", ".", "--command", "true"}
	cmd := exec.Command("nix", buildArgs...)
	cmd.Dir = meshRoot
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mesh v3 install prerequisites failed: %w", err)
	}
	fmt.Println("mesh v3 install complete")
	return nil
}
