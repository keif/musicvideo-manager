package main

import (
	"os"

	"github.com/keif/musicvideo-manager/internal/cli"
)

var version = "dev"

func main() {
	root := cli.NewRootCmd(version)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
