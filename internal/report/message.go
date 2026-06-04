// Package report renders human-facing summaries of UsageReport status, shared
// by the controller (status condition message) and the CLI so their framing
// stays identical.
package report

import (
	"fmt"
	"strings"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
)

// StatusMessage renders the utilization-aware framing for a UsageReport: the
// raw period cost, the amortized $/MTok with its utilization context, the
// marginal $/MTok (when known), and the break-even comparison against the first
// configured cloud target. Surfacing utilization inline keeps the amortized
// number from reading as "worse than every cloud" at low utilization.
func StatusMessage(s finopsv1alpha1.UsageReportStatus) string {
	totalTokens := s.InputTokens + s.OutputTokens
	var b strings.Builder

	fmt.Fprintf(&b, "Period %s: $%.4f across %s tokens.", s.Period, s.EstimatedCostUSD, withCommas(totalTokens))
	fmt.Fprintf(&b, "\nAmortized: $%.2f/MTok at %.0f%% utilization.", s.CostPerMillionTokens, s.UtilizationPercent)

	// Marginal is omitted when zero: that means no active energy was sampled, so
	// "$0.0000/MTok" would be misleading rather than honest.
	if s.MarginalCostPerMillionTokens > 0 {
		fmt.Fprintf(&b, "\nMarginal: $%.4f/MTok.", s.MarginalCostPerMillionTokens)
	}

	if len(s.BreakEvenAnalysis) > 0 {
		be := s.BreakEvenAnalysis[0]
		fmt.Fprintf(&b, "\nBreak-even with %s/%s: %s tokens/day (you served %s).",
			be.Provider, be.Model,
			humanizeTokens(be.BreakEvenTokensPerDay),
			humanizeTokens(be.CurrentUtilizationTokensPerDay))
	}

	return b.String()
}

// humanizeTokens renders a token count compactly: 950, 6K, 142K, 1.2M.
func humanizeTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000), ".0") + "K"
	default:
		return fmt.Sprintf("%d", n)
	}
}

// withCommas formats an integer with thousands separators (3993 -> "3,993").
func withCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
