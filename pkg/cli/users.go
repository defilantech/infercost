package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type usersOptions struct {
	namespace string
	top       int
}

// NewUsersCommand creates the "users" CLI subcommand for per-user inference cost
// attribution. Requires LiteLLM integration to be configured on the InferCost operator.
func NewUsersCommand() *cobra.Command {
	opts := &usersOptions{}

	cmd := &cobra.Command{
		Use:   "users",
		Short: "Show per-user inference cost attribution",
		Long: `Display per-user cost attribution for inference workloads.

Requires LiteLLM integration to be configured on the InferCost operator.
LiteLLM provides API key management and per-user request routing, which
InferCost uses to attribute costs to individual users.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsers(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().IntVar(&opts.top, "top", 10, "Number of top users to display")

	return cmd
}

func runUsers(_ *usersOptions) error {
	fmt.Println("Per-user cost tracking requires LiteLLM integration.")
	fmt.Println("Configure --litellm-db-dsn on the InferCost operator to enable per-user attribution.")
	fmt.Println("See https://infercost.ai/docs/litellm for setup instructions.")
	return nil
}
