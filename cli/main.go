package main

import (
	"os"

	meshv1 "dialtone/dev/mods/mesh/v1/go"
	logs "dialtone/dev/plugins/logs/src_v1/go"
)

func main() {
	logs.SetOutput(os.Stdout)
	if err := meshv1.Run(os.Args[1:]); err != nil {
		logs.Error("mesh error: %v", err)
		os.Exit(1)
	}
}
