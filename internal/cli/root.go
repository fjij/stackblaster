package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Version = "0.0.1-dev"

var rootCmd = &cobra.Command{
	Use:           "sb",
	Short:         "stackblaster — a Graphite-flavored CLI for GitHub stacked PRs",
	Version:       Version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
