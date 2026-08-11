package main

import (
	"os"

	"github.com/fjij/stackblaster/internal/cli"
)

func main() {
	if args, ok := cli.ShouldPassthrough(os.Args); ok {
		os.Exit(cli.RunGit(args))
	}
	cli.Execute()
}
