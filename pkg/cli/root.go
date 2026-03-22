package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCommand creates the root command for the infercost CLI.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infercost",
		Short: "True cost intelligence for on-prem AI inference",
		Long: `InferCost: Know the true cost of AI inference on your hardware.

Computes real cost-per-token from GPU hardware amortization, electricity,
and actual power draw. Compares against cloud API pricing to show savings.

InferCost works with any Kubernetes inference stack. First-class integration
with LLMKube, llama.cpp, and vLLM.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(NewStatusCommand())
	cmd.AddCommand(NewCompareCommand())
	cmd.AddCommand(NewVersionCommand())

	return cmd
}
