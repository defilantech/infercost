/*
Copyright 2026.
*/

package focus

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
)

func sampleProfile() finopsv1alpha1.CostProfile {
	return finopsv1alpha1.CostProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "shadowstack-5060ti"},
		Spec: finopsv1alpha1.CostProfileSpec{
			Hardware: finopsv1alpha1.HardwareSpec{
				GPUModel:          "NVIDIA GeForce RTX 5060 Ti",
				GPUCount:          2,
				PurchasePriceUSD:  960,
				AmortizationYears: 4,
			},
			Electricity: finopsv1alpha1.ElectricitySpec{
				RatePerKWh: 0.08,
				PUEFactor:  1.0,
			},
		},
	}
}

func sampleReport() finopsv1alpha1.UsageReport {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 20, 17, 0, 0, 0, time.UTC)
	return finopsv1alpha1.UsageReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "engineering-daily",
			Namespace: "engineering",
		},
		Spec: finopsv1alpha1.UsageReportSpec{
			CostProfileRef: "shadowstack-5060ti",
			Schedule:       finopsv1alpha1.ReportScheduleDaily,
		},
		Status: finopsv1alpha1.UsageReportStatus{
			Period:           "2026-04",
			PeriodStart:      &metav1.Time{Time: start},
			PeriodEnd:        &metav1.Time{Time: end},
			InputTokens:      1_200_000,
			OutputTokens:     800_000,
			EstimatedCostUSD: 0.8941,
			ByModel: []finopsv1alpha1.ModelCostBreakdown{
				{
					Model:            "qwen3-coder-30b",
					Namespace:        "engineering",
					InputTokens:      700_000,
					OutputTokens:     500_000,
					EstimatedCostUSD: 0.5364,
				},
				{
					Model:            "qwen3-8b",
					Namespace:        "engineering",
					InputTokens:      500_000,
					OutputTokens:     300_000,
					EstimatedCostUSD: 0.3577,
				},
			},
			CloudComparison: []finopsv1alpha1.CloudComparisonEntry{
				{
					Provider:              "Anthropic",
					Model:                 "claude-sonnet-4-6",
					EstimatedCloudCostUSD: 15.60,
					SavingsUSD:            14.70,
					SavingsPercent:        94.2,
				},
				{
					Provider:              "OpenAI",
					Model:                 "gpt-5.4",
					EstimatedCloudCostUSD: 15.00,
					SavingsUSD:            14.10,
					SavingsPercent:        94.0,
				},
			},
		},
	}
}

func parseCSV(t *testing.T, data []byte) ([]string, [][]string) {
	t.Helper()
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parsing CSV: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("empty CSV")
	}
	return records[0], records[1:]
}

func columnIdx(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

func TestWrite_HeaderContainsFOCUSStandardAndExtensions(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	header, _ := parseCSV(t, buf.Bytes())

	// Standard FOCUS v1 columns every consumer expects.
	for _, c := range []string{
		"BilledCost", "EffectiveCost", "ChargePeriodStart", "ChargePeriodEnd",
		"Currency", "ServiceName", "ServiceCategory", "ResourceId", "UsageQuantity",
		"UsageUnit", "SubAccountId", "Tags",
	} {
		if columnIdx(header, c) < 0 {
			t.Errorf("missing standard FOCUS column %q", c)
		}
	}

	// InferCost extensions with the FOCUS-required x- prefix.
	for _, c := range []string{
		"x-InferGpuModel", "x-InferTokensInput", "x-InferTokensOutput",
		"x-InferPUEFactor", "x-InferCloudEquivalentProvider", "x-InferSavingsPercent",
	} {
		if columnIdx(header, c) < 0 {
			t.Errorf("missing InferCost extension column %q", c)
		}
	}
}

func TestWrite_OneRowPerModel(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	_, rows := parseCSV(t, buf.Bytes())
	if len(rows) != 2 {
		t.Fatalf("expected 2 model rows, got %d", len(rows))
	}
}

func TestWrite_RolluprowWhenNoModelBreakdown(t *testing.T) {
	r := sampleReport()
	r.Status.ByModel = nil

	var buf bytes.Buffer
	err := Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{r},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, rows := parseCSV(t, buf.Bytes())
	if len(rows) != 1 {
		t.Fatalf("expected 1 rollup row when ByModel is empty, got %d", len(rows))
	}
}

func TestWrite_BilledCostMatchesModelBreakdown(t *testing.T) {
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})

	header, rows := parseCSV(t, buf.Bytes())
	billedIdx := columnIdx(header, "BilledCost")
	skuIdx := columnIdx(header, "SkuId")

	for _, row := range rows {
		switch row[skuIdx] {
		case "qwen3-coder-30b":
			if row[billedIdx] != "0.5364" {
				t.Errorf("qwen3-coder-30b BilledCost = %s, want 0.5364", row[billedIdx])
			}
		case "qwen3-8b":
			if row[billedIdx] != "0.3577" {
				t.Errorf("qwen3-8b BilledCost = %s, want 0.3577", row[billedIdx])
			}
		}
	}
}

func TestWrite_CloudEquivalentPicksHighestSavings(t *testing.T) {
	// Sample has Anthropic ($14.70 savings) and OpenAI ($14.10). Anthropic wins.
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	header, rows := parseCSV(t, buf.Bytes())
	provIdx := columnIdx(header, "x-InferCloudEquivalentProvider")
	for _, row := range rows {
		if row[provIdx] != "Anthropic" {
			t.Errorf("x-InferCloudEquivalentProvider = %q, want Anthropic (highest savings)", row[provIdx])
		}
	}
}

func TestWrite_PeriodEmittedAsRFC3339(t *testing.T) {
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	header, rows := parseCSV(t, buf.Bytes())
	startIdx := columnIdx(header, "ChargePeriodStart")
	endIdx := columnIdx(header, "ChargePeriodEnd")
	for _, row := range rows {
		if _, err := time.Parse(time.RFC3339, row[startIdx]); err != nil {
			t.Errorf("ChargePeriodStart %q not RFC3339: %v", row[startIdx], err)
		}
		if _, err := time.Parse(time.RFC3339, row[endIdx]); err != nil {
			t.Errorf("ChargePeriodEnd %q not RFC3339: %v", row[endIdx], err)
		}
	}
}

func TestWrite_CurrencyAlwaysUSD(t *testing.T) {
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	header, rows := parseCSV(t, buf.Bytes())
	curIdx := columnIdx(header, "Currency")
	for _, row := range rows {
		if row[curIdx] != "USD" {
			t.Errorf("Currency = %q, want USD", row[curIdx])
		}
	}
}

func TestWrite_ServiceCategoryMatchesFOCUSEnum(t *testing.T) {
	// FOCUS v1 defines "AI and Machine Learning" as a canonical
	// ServiceCategory value. Drifting from the enum breaks downstream
	// importers that validate categories.
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{sampleReport()},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	header, rows := parseCSV(t, buf.Bytes())
	idx := columnIdx(header, "ServiceCategory")
	for _, row := range rows {
		if row[idx] != "AI and Machine Learning" {
			t.Errorf("ServiceCategory = %q, want 'AI and Machine Learning'", row[idx])
		}
	}
}

func TestWrite_MultipleReportsSortedDeterministically(t *testing.T) {
	r1 := sampleReport()
	r1.Name = "engineering-daily"
	r1.Namespace = "engineering"
	r2 := sampleReport()
	r2.Name = "research-daily"
	r2.Namespace = "research"

	// Shuffle input order.
	var buf bytes.Buffer
	_ = Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{r2, r1},
		Profiles: map[string]finopsv1alpha1.CostProfile{"shadowstack-5060ti": sampleProfile()},
	})
	header, rows := parseCSV(t, buf.Bytes())
	subIdx := columnIdx(header, "SubAccountId")
	// Engineering must come before Research even though we fed them in r2,r1.
	if rows[0][subIdx] != "engineering" {
		t.Errorf("first row SubAccountId = %q, want engineering", rows[0][subIdx])
	}
}

func TestWrite_MissingProfileDoesNotCrash(t *testing.T) {
	r := sampleReport()
	r.Spec.CostProfileRef = "does-not-exist"

	var buf bytes.Buffer
	if err := Write(&buf, ExportInput{
		Reports:  []finopsv1alpha1.UsageReport{r},
		Profiles: map[string]finopsv1alpha1.CostProfile{},
	}); err != nil {
		t.Fatalf("Write failed on missing profile: %v", err)
	}

	header, rows := parseCSV(t, buf.Bytes())
	gpuIdx := columnIdx(header, "x-InferGpuModel")
	for _, row := range rows {
		if row[gpuIdx] != "" {
			t.Errorf("expected empty x-InferGpuModel for missing profile, got %q", row[gpuIdx])
		}
	}
}

func TestWrite_EmptyInputEmitsOnlyHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, ExportInput{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	output := buf.String()
	if !strings.HasPrefix(output, "BilledCost,") {
		t.Errorf("output should start with header, got: %s", output[:min(60, len(output))])
	}
	if strings.Count(output, "\n") != 1 {
		t.Errorf("expected only the header line, got %d lines", strings.Count(output, "\n"))
	}
}
