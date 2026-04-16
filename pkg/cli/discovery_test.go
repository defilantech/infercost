package cli

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestMetricsURLFromEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "service DNS with chat completions path",
			endpoint: "http://openclaw-finance.default.svc.cluster.local:8080/v1/chat/completions",
			want:     "http://openclaw-finance.default.svc.cluster.local:8080/metrics",
		},
		{
			name:     "raw IP with path",
			endpoint: "http://100.103.147.52:8080/v1/chat/completions",
			want:     "http://100.103.147.52:8080/metrics",
		},
		{
			name:     "different port",
			endpoint: "http://host:9090/v1/completions",
			want:     "http://host:9090/metrics",
		},
		{
			name:     "no path",
			endpoint: "http://host:8080",
			want:     "http://host:8080/metrics",
		},
		{
			name:     "strips query and fragment",
			endpoint: "http://host:8080/v1/completions?foo=bar#section",
			want:     "http://host:8080/metrics",
		},
		{
			name:     "empty string",
			endpoint: "",
			wantErr:  true,
		},
		{
			name:     "no host",
			endpoint: "/v1/chat/completions",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := metricsURLFromEndpoint(tt.endpoint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeduplicationKey(t *testing.T) {
	if got := deduplicationKey("default", "llama-70b"); got != "default/llama-70b" {
		t.Errorf("got %q, want %q", got, "default/llama-70b")
	}
}

func TestDiscoverModelsFromPods(t *testing.T) {
	cfg := &rest.Config{Host: "https://k8s.example.com"}

	tests := []struct {
		name          string
		pods          []corev1.Pod
		wantModels    int
		wantKnown     int
		wantScrapable int
	}{
		{
			name:       "no pods",
			pods:       nil,
			wantModels: 0,
			wantKnown:  0,
		},
		{
			name: "no labeled pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
				},
			},
			wantModels: 0,
			wantKnown:  0,
		},
		{
			name: "one running pod with label",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-pod",
						Namespace: "default",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
				},
			},
			wantModels:    1,
			wantKnown:     1,
			wantScrapable: 1,
		},
		{
			name: "pending pod",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-pod",
						Namespace: "default",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			wantModels:    1,
			wantKnown:     1,
			wantScrapable: 0,
		},
		{
			name: "running pod without IP",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-pod",
						Namespace: "default",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ""},
				},
			},
			wantModels:    1,
			wantKnown:     1,
			wantScrapable: 0,
		},
		{
			name: "two pods same model different namespaces",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-a",
						Namespace: "prod",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-b",
						Namespace: "staging",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2"},
				},
			},
			wantModels:    2,
			wantKnown:     2,
			wantScrapable: 2,
		},
		{
			name: "two pods same model same namespace",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-a",
						Namespace: "default",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "llama-b",
						Namespace: "default",
						Labels:    map[string]string{"inference.llmkube.dev/model": "llama-70b"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2"},
				},
			},
			wantModels:    2,
			wantKnown:     1, // same dedup key
			wantScrapable: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, known := discoverModelsFromPods(cfg, tt.pods)
			if len(models) != tt.wantModels {
				t.Errorf("models: got %d, want %d", len(models), tt.wantModels)
			}
			if len(known) != tt.wantKnown {
				t.Errorf("known: got %d, want %d", len(known), tt.wantKnown)
			}
			scrapable := 0
			for _, m := range models {
				if m.IsScrapable {
					scrapable++
				}
				if m.Source != "pod" {
					t.Errorf("expected source 'pod', got %q", m.Source)
				}
			}
			if scrapable != tt.wantScrapable {
				t.Errorf("scrapable: got %d, want %d", scrapable, tt.wantScrapable)
			}
		})
	}
}

func newFakeInferenceService(name, modelRef, phase, endpoint string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "inference.llmkube.dev",
		Version: "v1alpha1",
		Kind:    "InferenceService",
	})
	obj.SetName(name)
	obj.SetNamespace("default")
	obj.Object["spec"] = map[string]any{
		"modelRef": modelRef,
	}
	obj.Object["status"] = map[string]any{
		"phase":    phase,
		"endpoint": endpoint,
	}
	return obj
}

func newFakeDynamicClient(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	runtimeObjects := make([]runtime.Object, 0, len(objects))
	for _, obj := range objects {
		runtimeObjects = append(runtimeObjects, obj)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			inferenceServiceGVR: "InferenceServiceList",
		},
		runtimeObjects...,
	)
}

func TestDiscoverModelsFromInferenceServices(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		objects     []*unstructured.Unstructured
		knownModels map[string]bool
		namespace   string
		wantModels  int
	}{
		{
			name:        "no InferenceServices",
			objects:     nil,
			knownModels: map[string]bool{},
			wantModels:  0,
		},
		{
			name: "one Ready ISVC not in known",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("openclaw-finance", "qwen3-30b-a3b", "Ready",
					"http://openclaw-finance.default.svc.cluster.local:8080/v1/chat/completions"),
			},
			knownModels: map[string]bool{},
			wantModels:  1,
		},
		{
			name: "Ready ISVC already known from pod — deduplicated",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("nomic-embed", "nomic-embed", "Ready",
					"http://nomic-embed.default.svc.cluster.local:8080/v1/chat/completions"),
			},
			knownModels: map[string]bool{"default/nomic-embed": true},
			wantModels:  0,
		},
		{
			name: "not Ready — skipped",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("openclaw-llm", "qwen3-32b", "Creating",
					"http://openclaw-llm.default.svc.cluster.local:8080/v1/chat/completions"),
			},
			knownModels: map[string]bool{},
			wantModels:  0,
		},
		{
			name: "no endpoint — skipped",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("broken", "broken-model", "Ready", ""),
			},
			knownModels: map[string]bool{},
			wantModels:  0,
		},
		{
			name: "no modelRef — skipped",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("empty", "", "Ready",
					"http://host:8080/v1/chat/completions"),
			},
			knownModels: map[string]bool{},
			wantModels:  0,
		},
		{
			name: "mixed: Ready+unknown, Ready+known, Creating",
			objects: []*unstructured.Unstructured{
				newFakeInferenceService("finance", "qwen3-30b", "Ready",
					"http://finance:8080/v1/chat/completions"),
				newFakeInferenceService("embed", "nomic-embed", "Ready",
					"http://embed:8080/v1/chat/completions"),
				newFakeInferenceService("pending", "qwen3-32b", "Creating",
					"http://pending:8080/v1/chat/completions"),
			},
			knownModels: map[string]bool{"default/nomic-embed": true},
			wantModels:  1, // only finance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeDynamicClient(tt.objects...)
			models, err := discoverModelsFromInferenceServices(ctx, client, nil, tt.namespace, tt.knownModels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(models) != tt.wantModels {
				t.Errorf("got %d models, want %d", len(models), tt.wantModels)
			}
			for _, m := range models {
				if m.Source != sourceInferenceService {
					t.Errorf("expected source %q, got %q", sourceInferenceService, m.Source)
				}
				if m.MetricsURL == "" {
					t.Error("expected non-empty MetricsURL")
				}
			}
		})
	}
}

func TestDiscoverModelsFromInferenceServices_CRDNotInstalled(t *testing.T) {
	ctx := context.Background()

	// Create a client with the GVR registered but use a reactor to simulate
	// a 404 "resource not found" error, which is what a real cluster returns
	// when the InferenceService CRD is not installed.
	client := newFakeDynamicClient() // no objects, but GVR registered
	client.PrependReactor("list", "inferenceservices", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(inferenceServiceGVR.GroupResource(), "")
	})

	models, err := discoverModelsFromInferenceServices(ctx, client, nil, "", map[string]bool{})
	if err != nil {
		t.Fatalf("expected nil error for missing CRD, got: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestDiscoverModelsFromInferenceServices_RBACDenied(t *testing.T) {
	ctx := context.Background()

	client := newFakeDynamicClient()
	client.PrependReactor("list", "inferenceservices", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(inferenceServiceGVR.GroupResource(), "", nil)
	})

	models, err := discoverModelsFromInferenceServices(ctx, client, nil, "", map[string]bool{})
	if err != nil {
		t.Fatalf("expected nil error for RBAC denied, got: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestInferenceServiceToModel(t *testing.T) {
	obj := newFakeInferenceService("openclaw-finance", "qwen3-30b-a3b", "Ready",
		"http://100.103.147.52:8080/v1/chat/completions")

	m, ok := inferenceServiceToModel(obj, map[string]bool{})
	if !ok {
		t.Fatal("expected model to be returned")
	}
	if m.Name != "qwen3-30b-a3b" {
		t.Errorf("name: got %q, want %q", m.Name, "qwen3-30b-a3b")
	}
	if m.Namespace != "default" {
		t.Errorf("namespace: got %q, want %q", m.Namespace, "default")
	}
	if m.SourceName != "isvc/openclaw-finance" {
		t.Errorf("sourceName: got %q, want %q", m.SourceName, "isvc/openclaw-finance")
	}
	if m.MetricsURL != "http://100.103.147.52:8080/metrics" {
		t.Errorf("metricsURL: got %q, want %q", m.MetricsURL, "http://100.103.147.52:8080/metrics")
	}
	if !m.IsScrapable {
		t.Error("expected IsScrapable to be true")
	}
}
