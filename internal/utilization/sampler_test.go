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
		s.Record("cluster/a", 200, 50)
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
		s.Record("cluster/a", 20, 50)
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
	s.Record("cluster/a", 200, 50) // active
	clock.advance(30 * time.Minute)
	s.Record("cluster/a", 200, 50) // active
	clock.advance(30 * time.Minute)
	s.Record("cluster/a", 20, 50) // idle
	clock.advance(30 * time.Minute)
	s.Record("cluster/a", 20, 50) // idle
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

	s.Record("cluster/a", 100, 50)
	clock.advance(2 * time.Hour)
	s.Record("cluster/a", 100, 50)

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

	s.Record("cluster/a", 100, 50) // active under threshold 50
	clock.advance(30 * time.Minute)
	s.Record("cluster/a", 100, 200) // idle under raised threshold 200
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

	s.Record("cluster/a", 100, 50)
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
	s.Record("cluster/a", 300, 50)
	clock.advance(time.Hour)
	s.Record("cluster/a", 10, 50)
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
	s.Record("cluster/a", 100, 50)

	w := s.Summarize("cluster/a",
		time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 22, 6, 0, 0, 0, time.UTC),
	)
	if w.TotalHours != 0 || w.ActiveEnergyKWh != 0 {
		t.Fatalf("window before any samples should return zero summary, got %+v", w)
	}
}
