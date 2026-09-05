/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utilization

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_AppendAndLoadRange(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		sample := Sample{
			At:      base.Add(time.Duration(i) * 30 * time.Minute),
			PowerW:  float64(100 + i*10),
			ActiveW: 50,
		}
		if err := store.Append("cluster/a", sample); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	got, err := store.LoadRange("cluster/a", base, base.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 samples in [start, 2h), got %d", len(got))
	}
	// Chronological order.
	for i := 1; i < len(got); i++ {
		if got[i].At.Before(got[i-1].At) {
			t.Fatalf("samples not in chronological order at %d", i)
		}
	}
	// Power value round-trips.
	if got[0].PowerW != 100 || got[3].PowerW != 130 {
		t.Fatalf("power values not preserved: %+v", got)
	}
	// Active threshold round-trips (the per-sample classification).
	if got[0].ActiveW != 50 {
		t.Fatalf("active threshold not preserved: %+v", got[0])
	}
}

func TestStore_LoadRangeNoUpperBound(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		if err := store.Append("cluster/b", Sample{
			At:      base.Add(time.Duration(i) * time.Hour),
			PowerW:  200,
			ActiveW: 50,
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	got, err := store.LoadRange("cluster/b", base.Add(time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 samples after start with no upper bound, got %d", len(got))
	}
}

func TestStore_KeysAndPrune(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	// Two keys; each with one old and one new sample.
	for _, key := range []string{"cluster/x", "cluster/y"} {
		if err := store.Append(key, Sample{At: base.Add(-2 * time.Hour), PowerW: 10, ActiveW: 50}); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(key, Sample{At: base, PowerW: 10, ActiveW: 50}); err != nil {
			t.Fatal(err)
		}
	}

	keys, err := store.Keys()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}

	// Prune everything older than base.
	if err := store.Prune(base.Add(-time.Hour)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, key := range keys {
		got, err := store.LoadRange(key, base.Add(-time.Hour), time.Time{})
		if err != nil {
			t.Fatalf("LoadRange %s: %v", key, err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 sample to remain after prune, got %d for %s", len(got), key)
		}
		if !got[0].At.Equal(base) {
			t.Fatalf("remaining sample should be the new one, got %v", got[0].At)
		}
	}
}

func TestStore_RestartAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "samples.db")
	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Append("cluster/z", Sample{At: base, PowerW: 250, ActiveW: 50}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen as if the controller restarted.
	store2, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = store2.Close() }()
	got, err := store2.LoadRange("cluster/z", base, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("LoadRange after reopen: %v", err)
	}
	if len(got) != 1 || got[0].PowerW != 250 {
		t.Fatalf("sample did not survive reopen: %+v", got)
	}
}

func TestStore_CloseIsIdempotentAndNilSafe(t *testing.T) {
	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil store Close should be a no-op, got %v", err)
	}

	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
}

func TestStore_KeysEmptyStoreIsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	keys, err := store.Keys()
	if err != nil {
		t.Fatalf("Keys on empty store: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no keys on empty store, got %v", keys)
	}
}

func TestStore_PruneMissingKeyIsNoop(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Prune(time.Now().UTC().Add(-24 * time.Hour)); err != nil {
		t.Fatalf("Prune on empty store should be a no-op, got %v", err)
	}
}

func TestStore_AppendAfterCloseReturnsError(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Writing after Close must surface an error rather than silently drop the
	// sample, because the Sampler treats a Record error as a persistence failure.
	if err := store.Append("cluster/a", Sample{At: time.Now(), PowerW: 100, ActiveW: 50}); err == nil {
		t.Fatalf("expected error appending to a closed store")
	}
}

func TestStore_RecordsTrackThresholdAtWriteTime(t *testing.T) {
	// The per-sample ActiveW must be preserved independently, so a later change
	// to the profile's idle threshold never rewrites how an earlier sample was
	// classified. This is the reason persistence stores ActiveW rather than
	// re-deriving it at read time.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if err := store.Append("cluster/a", Sample{At: base, PowerW: 100, ActiveW: 50}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := store.LoadRange("cluster/a", base, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(got))
	}
	if got[0].ActiveW != 50 {
		t.Fatalf("ActiveW not preserved: %+v", got[0])
	}
}
