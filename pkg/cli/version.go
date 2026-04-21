package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Set via ldflags at build time.
	// Version is the release version string (defaults to "dev").
	Version = "dev"
	// GitCommit is the git commit hash the binary was built from (defaults to "unknown").
	GitCommit = "unknown"
	// BuildDate is the date/time the binary was built (defaults to "unknown").
	BuildDate = "unknown"
)

// NewVersionCommand creates the "version" CLI subcommand that prints the
// InferCost version, git commit, and build date.
func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the InferCost version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("infercost %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
		},
	}
}
