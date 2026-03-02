package main

import (
	"os"

	logs "dialtone/dev/plugins/logs/src_v1/go"
	meshv1 "dialtone/dev/plugins/mesh/v1/go"
)

func main() {
	logs.SetOutput(os.Stdout)
	if err := meshv1.Run(os.Args[1:]); err != nil {
		logs.Error("mesh error: %v", err)
		os.Exit(1)
	}
}
