package main

import (
	"os"

	"github.com/fjij/stackblaster/cmd"
)

func main() {
	if args, ok := cmd.ShouldPassthrough(os.Args); ok {
		os.Exit(cmd.RunGit(args))
	}
	cmd.Execute()
}
