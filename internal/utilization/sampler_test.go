/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package utilization

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// fixedClock returns times in deterministic order for the Sampler's internal now().
type fixedClock struct {
	cur time.Time
}

func (c *fixedClock) now() time.Time { return c.cur }
func (c *fixedClock) advance(d time.Duration) {
	c.cur = c.cur.Add(d)
}

// approxEq avoids brittle float equality across platforms.
func approxEq(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s: got %v, want %v", label, got, want)
	}
}

// mustRecord records a sample for the cluster/a profile and fails the test on a
// persistence error.
func mustRecord(t *testing.T, s *Sampler, powerW, activeW float64) {
	t.Helper()
	if err := s.Record("cluster/a", powerW, activeW); err != nil {
		t.Fatalf("Record(cluster/a): %v", err)
	}
}

func TestSampler_EmptyKeyReturnsZeroAndZero(t *testing.T) {
	s := NewSampler()
	active, total := s.ActiveAndTotalHours("missing", time.Now().Add(-time.Hour), time.Now())
	if active != 0 || total != 0 {
		t.Fatalf("empty sampler should return 0,0 — got active=%v total=%v", active, total)
	}
}

func TestSampler_ActiveAboveThreshold(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now

	// Record 4 samples at 30-minute intervals, all above the 50W threshold.
	// Expect activeHours ≈ totalHours ≈ 2h (the gaps between samples +
	// the tail gap from last sample to `end`).
	for range 4 {
		mustRecord(t, s, 200, 50)
		clock.advance(30 * time.Minute)
	}
	active, total := s.ActiveAndTotalHours("cluster/a",
		time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		clock.now())

	// 3 gaps × 0.5h = 1.5h between samples, no leading gap (first sample is at start).
	// Tail: from last sample to `end` is 0 because `end` == clock.now() at the
	// moment Record was last called. But after the loop we advanced 30m past
	// the last Record, so last sample is 30m before end — that's an extra
	// 0.5h tail. Total = 2.0h.
	approxEq(t, total, 2.0, "total hours")
	approxEq(t, active, 2.0, "active hours (all above threshold)")
}

func TestSampler_IdleBelowThreshold(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now

	// All samples below threshold — expect totalHours > 0, activeHours == 0.
	for range 4 {
		mustRecord(t, s, 20, 50)
		clock.advance(30 * time.Minute)
	}
	active, total := s.ActiveAndTotalHours("cluster/a",
		time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		clock.now())

	if total <= 0 {
		t.Fatalf("expected non-zero total hours with samples present")
	}
	if active != 0 {
		t.Fatalf("expected 0 active hours when all samples are below threshold, got %v", active)
	}
}

func TestSampler_MixedActiveAndIdle(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now
	start := clock.now()

	// 2 active samples + 2 idle samples, each 30 minutes apart.
	// Expect roughly half active, half idle.
	mustRecord(t, s, 200, 50) // active
	clock.advance(30 * time.Minute)
	mustRecord(t, s, 200, 50) // active
	clock.advance(30 * time.Minute)
	mustRecord(t, s, 20, 50) // idle
	clock.advance(30 * time.Minute)
	mustRecord(t, s, 20, 50) // idle
	clock.advance(30 * time.Minute)

	active, total := s.ActiveAndTotalHours("cluster/a", start, clock.now())

	// 4 30-min intervals = 2h total. First interval attributed to the
	// "active" sample, etc. We expect ~1h active.
	approxEq(t, total, 2.0, "total hours")
	if active < 0.9 || active > 1.1 {
		t.Fatalf("expected ~1h active in mixed workload, got %v", active)
	}
}

func TestSampler_RetentionDropsOldSamples(t *testing.T) {
	s := NewSamplerWithRetention(1 * time.Hour)
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now

	mustRecord(t, s, 100, 50)
	clock.advance(2 * time.Hour)
	mustRecord(t, s, 100, 50)

	snap := s.Snapshot("cluster/a")
	if len(snap) != 1 {
		t.Fatalf("expected retention to drop the older sample, got %d samples", len(snap))
	}
}

func TestSampler_ThresholdChangeAtRecordTime(t *testing.T) {
	// If the operator raises the idleWattsThreshold partway through a period,
	// previously-recorded samples keep their original classification (stored
	// on the sample). Only new samples pick up the new threshold.
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now
	start := clock.now()

	mustRecord(t, s, 100, 50) // active under threshold 50
	clock.advance(30 * time.Minute)
	mustRecord(t, s, 100, 200) // idle under raised threshold 200
	clock.advance(30 * time.Minute)

	active, total := s.ActiveAndTotalHours("cluster/a", start, clock.now())
	approxEq(t, total, 1.0, "total hours across the two intervals")
	if active < 0.4 || active > 0.6 {
		t.Fatalf("expected ~0.5h active across the threshold change, got %v", active)
	}
}

func TestSampler_WindowBeforeAnySamples(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)}
	s.now = clock.now

	mustRecord(t, s, 100, 50)
	// Ask about a window entirely in the past.
	active, total := s.ActiveAndTotalHours("cluster/a",
		time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 22, 6, 0, 0, 0, time.UTC),
	)
	if active != 0 || total != 0 {
		t.Fatalf("window entirely before samples should return 0,0 — got active=%v total=%v", active, total)
	}
}

func TestDefaultIdleThresholdWatts(t *testing.T) {
	tdp := int32(150)
	got := DefaultIdleThresholdWatts(&tdp, 2)
	want := 150 * 0.2 * 2
	approxEq(t, got, want, "150W TDP x 2 GPUs at 20%")

	got = DefaultIdleThresholdWatts(nil, 2)
	approxEq(t, got, 60, "nil TDP falls back to 30W × 2")

	got = DefaultIdleThresholdWatts(nil, 0)
	approxEq(t, got, 30, "zero GPU count clamps to 1 for floor")
}

func TestSampler_SummarizeEnergy(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now
	start := clock.now()

	// 300 W for 1 hour, then 10 W for 1 hour. Threshold 50 W.
	// Active: first hour only. Active energy: 0.3 kWh. Total: 0.31 kWh.
	mustRecord(t, s, 300, 50)
	clock.advance(time.Hour)
	mustRecord(t, s, 10, 50)
	clock.advance(time.Hour)

	w := s.Summarize("cluster/a", start, clock.now())
	approxEq(t, w.TotalHours, 2.0, "total hours across the two intervals")
	approxEq(t, w.ActiveHours, 1.0, "active hours (first sample above threshold only)")
	approxEq(t, w.ActiveEnergyKWh, 0.3, "active energy: 300W × 1h = 300Wh = 0.3kWh")
	approxEq(t, w.TotalEnergyKWh, 0.31, "total energy: 0.3 + (10W × 1h / 1000) = 0.31 kWh")
}

func TestSampler_SummarizeNoOverlapReturnsZero(t *testing.T) {
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)}
	s.now = clock.now
	mustRecord(t, s, 100, 50)

	w := s.Summarize("cluster/a",
		time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 22, 6, 0, 0, 0, time.UTC),
	)
	if w.TotalHours != 0 || w.ActiveEnergyKWh != 0 {
		t.Fatalf("window before any samples should return zero summary, got %+v", w)
	}
}

func TestSampler_PersistsAcrossRestart(t *testing.T) {
	// Record samples to a durable store, then rehydrate a new Sampler as if the
	// controller restarted. The rehydrated sampler must report the same active
	// hours and energy for the same window, and must keep samples within the
	// (store) retention rather than the in-memory default.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	clock := &fixedClock{cur: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	s, err := newSamplerWithStore(DefaultStoreRetention, store, clock.now)
	if err != nil {
		t.Fatalf("newSamplerWithStore: %v", err)
	}
	start := clock.now()

	if err := s.Record("cluster/a", 300, 50); err != nil {
		t.Fatalf("Record active: %v", err)
	}
	clock.advance(time.Hour)
	if err := s.Record("cluster/a", 10, 50); err != nil {
		t.Fatalf("Record idle: %v", err)
	}
	clock.advance(time.Hour)

	want := s.Summarize("cluster/a", start, clock.now())
	if want.ActiveHours != 1.0 || want.ActiveEnergyKWh != 0.3 {
		t.Fatalf("unexpected pre-restart summary: %+v", want)
	}

	// Simulate restart: close store, reopen, rehydrate a fresh sampler. It must
	// hydrate using the same clock so the retention window is deterministic.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	store2, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	s2, err := newSamplerWithStore(DefaultStoreRetention, store2, clock.now)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	defer func() { _ = s2.Close() }()

	got := s2.Summarize("cluster/a", start, clock.now())
	approxEq(t, got.ActiveHours, want.ActiveHours, "active hours after restart")
	approxEq(t, got.ActiveEnergyKWh, want.ActiveEnergyKWh, "active energy after restart")
	approxEq(t, got.TotalHours, want.TotalHours, "total hours after restart")
	if len(s2.Snapshot("cluster/a")) != 2 {
		t.Fatalf("expected both samples after restart, got %d", len(s2.Snapshot("cluster/a")))
	}
}

func TestSampler_InMemoryWhenNoStore(t *testing.T) {
	// A Sampler built without a store keeps the 48h default window and records
	// to memory only; Record must not return an error and must not persist.
	s := NewSampler()
	clock := &fixedClock{cur: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now
	if err := s.Record("cluster/a", 100, 50); err != nil {
		t.Fatalf("in-memory Record returned error: %v", err)
	}
	if s.store != nil {
		t.Fatalf("expected no store for in-memory sampler")
	}
}

func TestSampler_CloseNilSafeOnInMemory(t *testing.T) {
	// Close on a pure in-memory sampler has no store to release and must be a
	// no-op, and callable more than once.
	s := NewSampler()
	if err := s.Close(); err != nil {
		t.Fatalf("Close on in-memory sampler: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close on in-memory sampler: %v", err)
	}
}

func TestSampler_NewSamplerWithStoreReturnsErrorOnHydrateFailure(t *testing.T) {
	// Hydration fails when the backing store is closed; the constructor must
	// propagate that so main can fail loud rather than start with an empty
	// history window silently.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := NewSamplerWithStore(DefaultStoreRetention, store); err == nil {
		t.Fatalf("expected error hydrating from a closed store")
	}
}

func TestSampler_RecordSurfacesPersistenceError(t *testing.T) {
	// When the store write fails, Record must return the error (so the
	// controller can log it) and must NOT add the sample to memory, keeping the
	// in-memory window in sync with what is durable.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	s, err := NewSamplerWithStore(DefaultStoreRetention, store)
	if err != nil {
		t.Fatalf("NewSamplerWithStore: %v", err)
	}
	clock := &fixedClock{cur: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Record("cluster/a", 100, 50); err == nil {
		t.Fatalf("expected Record to return an error after store close")
	}
	if got := len(s.Snapshot("cluster/a")); got != 0 {
		t.Fatalf("failed Record must not mutate the in-memory window, got %d samples", got)
	}
}

func TestSampler_HydrateRespectsRetentionCutoff(t *testing.T) {
	// Rehydrating a sampler must only load samples within the retention window;
	// anything older is left in the store but not loaded, so the in-memory
	// window stays bounded by retention. An injected clock keeps the retention
	// window deterministic regardless of the wall clock.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	clock := &fixedClock{cur: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	recent := clock.now().Add(-time.Hour)
	old := clock.now().Add(-48 * time.Hour)
	if err := store.Append("cluster/a", Sample{At: old, PowerW: 100, ActiveW: 50}); err != nil {
		t.Fatalf("Append old: %v", err)
	}
	if err := store.Append("cluster/a", Sample{At: recent, PowerW: 200, ActiveW: 50}); err != nil {
		t.Fatalf("Append recent: %v", err)
	}

	// A short retention (24h) makes `old` fall outside the window.
	s, err := newSamplerWithStore(24*time.Hour, store, clock.now)
	if err != nil {
		t.Fatalf("newSamplerWithStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	samples := s.Snapshot("cluster/a")
	if len(samples) != 1 {
		t.Fatalf("expected only the recent sample within retention, got %d", len(samples))
	}
	if !samples[0].At.Equal(recent) {
		t.Fatalf("expected the recent sample, got %v", samples[0].At)
	}
}

func TestSampler_PrunesExpiredStoreSamples(t *testing.T) {
	// The durable store must not grow unbounded: once retention and the prune
	// throttle elapse, Record prunes samples older than the retention window.
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "samples.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	s, err := NewSamplerWithStore(2*time.Hour, store)
	if err != nil {
		t.Fatalf("NewSamplerWithStore: %v", err)
	}
	clock := &fixedClock{cur: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	s.now = clock.now

	now := clock.now()
	// First record: within retention and triggers an immediate (first) prune.
	if err := s.Record("cluster/a", 100, 50); err != nil {
		t.Fatalf("Record 0: %v", err)
	}
	// Advance past both retention (2h) and the prune throttle (15m), then record.
	clock.advance(3 * time.Hour)
	if err := s.Record("cluster/a", 200, 50); err != nil {
		t.Fatalf("Record 1: %v", err)
	}

	// The first sample is now older than retention + throttle, so it must have
	// been pruned from the store (not just the in-memory window). Load with no
	// upper bound; after a correct prune only the newer sample remains.
	got, err := store.LoadRange("cluster/a", now, time.Time{})
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 sample retained after prune, got %d: %+v", len(got), got)
	}
	if got[0].PowerW != 200 {
		t.Fatalf("expected the newer sample to survive, got %+v", got[0])
	}
}
