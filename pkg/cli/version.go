package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Set via ldflags at build time.
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the InferCost version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("infercost %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
		},
	}
}
