package report

import (
	"strings"
	"testing"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
)

func fullStatus() finopsv1alpha1.UsageReportStatus {
	return finopsv1alpha1.UsageReportStatus{
		Period:                       "2026-04-23",
		EstimatedCostUSD:             0.0660,
		InputTokens:                  2000,
		OutputTokens:                 1993,
		CostPerMillionTokens:         16.52,
		MarginalCostPerMillionTokens: 0.004,
		UtilizationPercent:           4.0,
		BreakEvenAnalysis: []finopsv1alpha1.BreakEvenEntry{
			{Provider: "Anthropic", Model: "claude-opus-4-6",
				BreakEvenTokensPerDay: 142000, CurrentUtilizationTokensPerDay: 6000,
				PercentOfBreakEven: 4.2, Verdict: "cloud-cheaper-at-current-utilization"},
		},
	}
}

func TestStatusMessage_FullFraming(t *testing.T) {
	msg := StatusMessage(fullStatus())
	wants := []string{
		"Period 2026-04-23: $0.0660 across 3,993 tokens.",
		"Amortized: $16.52/MTok at 4% utilization.",
		"Marginal: $0.0040/MTok.",
		"Break-even with Anthropic/claude-opus-4-6: 142K tokens/day (you served 6K).",
	}
	for _, w := range wants {
		if !strings.Contains(msg, w) {
			t.Errorf("message missing line %q\ngot:\n%s", w, msg)
		}
	}
}

func TestStatusMessage_NoBreakEvenLineWhenEmpty(t *testing.T) {
	s := fullStatus()
	s.BreakEvenAnalysis = nil
	msg := StatusMessage(s)
	if strings.Contains(msg, "Break-even") {
		t.Errorf("expected no break-even line when analysis is empty; got:\n%s", msg)
	}
}

func TestStatusMessage_NoMarginalLineWhenZero(t *testing.T) {
	s := fullStatus()
	s.MarginalCostPerMillionTokens = 0
	msg := StatusMessage(s)
	if strings.Contains(msg, "Marginal:") {
		t.Errorf("expected no marginal line when marginal is 0; got:\n%s", msg)
	}
}

func TestHumanizeTokens(t *testing.T) {
	cases := map[int64]string{
		950:     "950",
		6000:    "6K",
		142000:  "142K",
		1200000: "1.2M",
	}
	for in, want := range cases {
		if got := humanizeTokens(in); got != want {
			t.Errorf("humanizeTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestWithCommas(t *testing.T) {
	if got := withCommas(3993); got != "3,993" {
		t.Errorf("withCommas(3993) = %q, want 3,993", got)
	}
	if got := withCommas(1000000); got != "1,000,000" {
		t.Errorf("withCommas(1000000) = %q, want 1,000,000", got)
	}
}
