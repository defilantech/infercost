package cli

import (
	"bytes"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		name   string
		tokens float64
		want   string
	}{
		{"zero", 0, "0"},
		{"single digit", 5, "5"},
		{"hundreds", 999, "999"},
		{"exactly one thousand", 1_000, "1.0K"},
		{"thousands", 1_500, "1.5K"},
		{"tens of thousands", 42_300, "42.3K"},
		{"hundreds of thousands", 999_900, "999.9K"},
		{"exactly one million", 1_000_000, "1.0M"},
		{"millions", 7_500_000, "7.5M"},
		{"hundreds of millions", 123_456_789, "123.5M"},
		{"exactly one billion", 1_000_000_000, "1.0B"},
		{"billions", 2_500_000_000, "2.5B"},
		{"large billions", 42_000_000_000, "42.0B"},
		{"fractional below thousand", 0.5, "0"},
		{"negative", -100, "-100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTokenCount(tt.tokens)
			if got != tt.want {
				t.Errorf("formatTokenCount(%v) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"zero seconds", 0, "0s"},
		{"one second", time.Second, "1s"},
		{"thirty seconds", 30 * time.Second, "30s"},
		{"fifty-nine seconds", 59 * time.Second, "59s"},
		{"one minute", time.Minute, "1m"},
		{"five minutes", 5 * time.Minute, "5m"},
		{"fifty-nine minutes", 59 * time.Minute, "59m"},
		{"one hour", time.Hour, "1h"},
		{"three hours", 3 * time.Hour, "3h"},
		{"twenty-three hours", 23 * time.Hour, "23h"},
		{"one day", 24 * time.Hour, "1d"},
		{"three days", 72 * time.Hour, "3d"},
		{"thirty days", 720 * time.Hour, "30d"},
		{"boundary: just under a minute", 59*time.Second + 999*time.Millisecond, "59s"},
		{"boundary: just under an hour", 59*time.Minute + 59*time.Second, "59m"},
		{"boundary: just under a day", 23*time.Hour + 59*time.Minute, "23h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.duration)
			if got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestNewRootCommand_SubcommandTree(t *testing.T) {
	cmd := NewRootCommand()

	if cmd.Use != "infercost" {
		t.Errorf("root Use = %q, want %q", cmd.Use, "infercost")
	}
	if cmd.Short != "True cost intelligence for on-prem AI inference" {
		t.Errorf("root Short = %q, want %q", cmd.Short, "True cost intelligence for on-prem AI inference")
	}
	if !cmd.SilenceUsage {
		t.Error("root SilenceUsage should be true")
	}

	expected := map[string]bool{
		"status":  false,
		"compare": false,
		"budget":  false,
		"users":   false,
		"version": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected subcommand %q not found", name)
		}
	}
}

func TestNewRootCommand_HasNoRunE(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.RunE != nil {
		t.Error("root command should not have a RunE (it is a container command)")
	}
	if cmd.Run != nil {
		t.Error("root command should not have a Run (it is a container command)")
	}
}

func TestNewVersionCommand(t *testing.T) {
	cmd := NewVersionCommand()

	if cmd.Use != "version" {
		t.Errorf("Use = %q, want %q", cmd.Use, "version")
	}
	if cmd.Short != "Print the InferCost version" {
		t.Errorf("Short = %q, want %q", cmd.Short, "Print the InferCost version")
	}
}

func TestVersionCommand_DefaultValues(t *testing.T) {
	if Version != "dev" {
		t.Errorf("default Version = %q, want %q", Version, "dev")
	}
	if GitCommit != "unknown" {
		t.Errorf("default GitCommit = %q, want %q", GitCommit, "unknown")
	}
	if BuildDate != "unknown" {
		t.Errorf("default BuildDate = %q, want %q", BuildDate, "unknown")
	}
}

func TestVersionCommand_Output(t *testing.T) {
	cmd := NewRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Override stdout to capture fmt.Printf output from the version command.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = origStdout
		t.Fatalf("version command returned error: %v", err)
	}

	_ = w.Close()
	os.Stdout = origStdout

	var captured bytes.Buffer
	if _, err := captured.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}

	output := captured.String()
	want := "infercost dev (commit: unknown, built: unknown)\n"
	if output != want {
		t.Errorf("version output = %q, want %q", output, want)
	}
}

func TestVersionCommand_CustomValues(t *testing.T) {
	origVersion := Version
	origCommit := GitCommit
	origDate := BuildDate
	t.Cleanup(func() {
		Version = origVersion
		GitCommit = origCommit
		BuildDate = origDate
	})

	Version = "1.2.3"
	GitCommit = "abc123"
	BuildDate = "2026-01-01"

	cmd := NewRootCommand()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = origStdout
		t.Fatalf("version command returned error: %v", err)
	}

	_ = w.Close()
	os.Stdout = origStdout

	var captured bytes.Buffer
	if _, err := captured.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}

	output := captured.String()
	want := "infercost 1.2.3 (commit: abc123, built: 2026-01-01)\n"
	if output != want {
		t.Errorf("version output = %q, want %q", output, want)
	}
}

func TestNewStatusCommand_Flags(t *testing.T) {
	cmd := NewStatusCommand()

	if cmd.Use != "status" {
		t.Errorf("Use = %q, want %q", cmd.Use, "status")
	}

	nsFlag := cmd.Flags().Lookup("namespace")
	if nsFlag == nil {
		t.Fatal("expected --namespace flag to exist")
	}
	if nsFlag.Shorthand != "n" {
		t.Errorf("namespace shorthand = %q, want %q", nsFlag.Shorthand, "n")
	}
	if nsFlag.DefValue != "" {
		t.Errorf("namespace default = %q, want empty string", nsFlag.DefValue)
	}

	allNsFlag := cmd.Flags().Lookup("all-namespaces")
	if allNsFlag == nil {
		t.Fatal("expected --all-namespaces flag to exist")
	}
	if allNsFlag.Shorthand != "A" {
		t.Errorf("all-namespaces shorthand = %q, want %q", allNsFlag.Shorthand, "A")
	}
	if allNsFlag.DefValue != "false" {
		t.Errorf("all-namespaces default = %q, want %q", allNsFlag.DefValue, "false")
	}
}

func TestNewStatusCommand_HasRunE(t *testing.T) {
	cmd := NewStatusCommand()
	if cmd.RunE == nil {
		t.Error("status command should have RunE set")
	}
}

func TestNewCompareCommand_Flags(t *testing.T) {
	cmd := NewCompareCommand()

	if cmd.Use != "compare" {
		t.Errorf("Use = %q, want %q", cmd.Use, "compare")
	}
	if cmd.Short != "Compare on-prem costs to cloud API pricing" {
		t.Errorf("Short = %q, want %q", cmd.Short, "Compare on-prem costs to cloud API pricing")
	}

	nsFlag := cmd.Flags().Lookup("namespace")
	if nsFlag == nil {
		t.Fatal("expected --namespace flag to exist")
	}
	if nsFlag.Shorthand != "n" {
		t.Errorf("namespace shorthand = %q, want %q", nsFlag.Shorthand, "n")
	}
	if nsFlag.DefValue != "" {
		t.Errorf("namespace default = %q, want empty string", nsFlag.DefValue)
	}

	monthlyFlag := cmd.Flags().Lookup("monthly")
	if monthlyFlag == nil {
		t.Fatal("expected --monthly flag to exist")
	}
	if monthlyFlag.DefValue != "false" {
		t.Errorf("monthly default = %q, want %q", monthlyFlag.DefValue, "false")
	}
}

func TestNewCompareCommand_HasRunE(t *testing.T) {
	cmd := NewCompareCommand()
	if cmd.RunE == nil {
		t.Error("compare command should have RunE set")
	}
}

func TestNewBudgetCommand_HasRunE(t *testing.T) {
	cmd := NewBudgetCommand()
	if cmd.RunE == nil {
		t.Error("budget command should have RunE set")
	}
}

func TestNewBudgetCommand_Flags(t *testing.T) {
	cmd := NewBudgetCommand()

	if cmd.Use != "budget" {
		t.Errorf("Use = %q, want %q", cmd.Use, "budget")
	}
	if cmd.Short != "List TokenBudget resources and spend status" {
		t.Errorf("Short = %q, want %q", cmd.Short, "List TokenBudget resources and spend status")
	}

	nsFlag := cmd.Flags().Lookup("namespace")
	if nsFlag == nil {
		t.Fatal("expected --namespace flag to exist")
	}
	if nsFlag.Shorthand != "n" {
		t.Errorf("namespace shorthand = %q, want %q", nsFlag.Shorthand, "n")
	}
	if nsFlag.DefValue != "" {
		t.Errorf("namespace default = %q, want empty string", nsFlag.DefValue)
	}

	allNsFlag := cmd.Flags().Lookup("all-namespaces")
	if allNsFlag == nil {
		t.Fatal("expected --all-namespaces flag to exist")
	}
	if allNsFlag.Shorthand != "A" {
		t.Errorf("all-namespaces shorthand = %q, want %q", allNsFlag.Shorthand, "A")
	}
	if allNsFlag.DefValue != "false" {
		t.Errorf("all-namespaces default = %q, want %q", allNsFlag.DefValue, "false")
	}
}

func TestNewUsersCommand_HasRunE(t *testing.T) {
	cmd := NewUsersCommand()
	if cmd.RunE == nil {
		t.Error("users command should have RunE set")
	}
}

func TestNewUsersCommand_Flags(t *testing.T) {
	cmd := NewUsersCommand()

	if cmd.Use != "users" {
		t.Errorf("Use = %q, want %q", cmd.Use, "users")
	}
	if cmd.Short != "Show per-user inference cost attribution" {
		t.Errorf("Short = %q, want %q", cmd.Short, "Show per-user inference cost attribution")
	}

	nsFlag := cmd.Flags().Lookup("namespace")
	if nsFlag == nil {
		t.Fatal("expected --namespace flag to exist")
	}
	if nsFlag.Shorthand != "n" {
		t.Errorf("namespace shorthand = %q, want %q", nsFlag.Shorthand, "n")
	}

	topFlag := cmd.Flags().Lookup("top")
	if topFlag == nil {
		t.Fatal("expected --top flag to exist")
	}
	if topFlag.DefValue != "10" {
		t.Errorf("top default = %q, want %q", topFlag.DefValue, "10")
	}
}

func TestUsersCommand_Output(t *testing.T) {
	cmd := NewRootCommand()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	cmd.SetArgs([]string{"users"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = origStdout
		t.Fatalf("users command returned error: %v", err)
	}

	_ = w.Close()
	os.Stdout = origStdout

	var captured bytes.Buffer
	if _, err := captured.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}

	output := captured.String()
	if !contains(output, "LiteLLM integration") {
		t.Errorf("expected output to mention LiteLLM integration, got: %q", output)
	}
	if !contains(output, "infercost.ai/docs/litellm") {
		t.Errorf("expected output to contain docs URL, got: %q", output)
	}
}

func TestBudgetCommand_HelpDoesNotError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"budget", "--help"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("budget --help should not error: %v", err)
	}
}

func TestUsersCommand_HelpDoesNotError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"users", "--help"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("users --help should not error: %v", err)
	}
}

func TestBudgetStatus(t *testing.T) {
	tests := []struct {
		name       string
		conditions []interface{}
		want       string
	}{
		{
			name:       "no conditions",
			conditions: nil,
			want:       "OK",
		},
		{
			name: "warning condition true",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "BudgetWarning",
					"status": "True",
				},
			},
			want: "Warning",
		},
		{
			name: "exceeded condition true",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "BudgetExceeded",
					"status": "True",
				},
			},
			want: "Exceeded",
		},
		{
			name: "exceeded takes priority over warning",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "BudgetWarning",
					"status": "True",
				},
				map[string]interface{}{
					"type":   "BudgetExceeded",
					"status": "True",
				},
			},
			want: "Exceeded",
		},
		{
			name: "warning condition false",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "BudgetWarning",
					"status": "False",
				},
			},
			want: "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-budget",
						"namespace": "default",
					},
				},
			}
			if tt.conditions != nil {
				obj.Object["status"] = map[string]interface{}{
					"conditions": tt.conditions,
				}
			}

			got := budgetStatus(obj)
			if got != tt.want {
				t.Errorf("budgetStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewK8sClient_NoKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent/path/kubeconfig")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	_, err := newK8sClient()
	if err == nil {
		t.Fatal("expected error when kubeconfig does not exist, got nil")
	}
}

func TestRootCommand_HelpDoesNotError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--help"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help should not error: %v", err)
	}

	output := buf.String()
	if len(output) == 0 {
		t.Error("help output should not be empty")
	}
}

func TestStatusCommand_HelpDoesNotError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"status", "--help"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status --help should not error: %v", err)
	}
}

func TestCompareCommand_HelpDoesNotError(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"compare", "--help"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("compare --help should not error: %v", err)
	}
}

func TestRootCommand_UnknownSubcommandErrors(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"nonexistent"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for unknown subcommand, got nil")
	}
}
