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

// Package utilization records per-CostProfile power samples and answers the
// question "how many of the last N hours were active?". It exists so the
// UsageReport reconciler can distinguish a 4K-token day that idled all night
// from a 4K-token day where the GPUs were running flat out — same $/token
// amortized, very different stories.
//
// Samples are stored in-memory with a sliding retention window (default 48h).
// Controller restarts lose history; that's acceptable for a v1 since the
// steady-state for most UsageReport schedules (daily) only needs the last 24h
// of samples. A future iteration can persist to a ConfigMap or Prometheus if
// retention becomes a real ask.
package utilization

import (
	"sort"
	"sync"
	"time"
)

// DefaultRetention is how far back the Sampler keeps samples when no
// persistent store is configured. 48h is enough for daily and weekly-so-far
// reports; without a store, monthly reports fall back to a linear
// extrapolation of what's retained.
const DefaultRetention = 48 * time.Hour

// DefaultStoreRetention is how far back the Sampler keeps samples when a
// persistent Store is configured. It is a bit longer than a calendar month so
// daily, weekly, and monthly reports all compute exactly from recorded samples
// rather than extrapolating from a short in-memory window.
const DefaultStoreRetention = 45 * 24 * time.Hour

// pruneInterval throttles store pruning so it doesn't run a full-scan delete
// on every Record (which fires every 30s per CostProfile). Samples are pruned
// at most this often; the worst-case over-retention is bounded by this window.
const pruneInterval = 15 * time.Minute

// Sample is a single power-draw observation for a CostProfile.
type Sample struct {
	At      time.Time
	PowerW  float64
	ActiveW float64 // threshold in effect at record time — used so threshold changes don't retroactively relabel old samples
}

// Sampler is concurrency-safe. A single Sampler instance is shared between the
// CostProfile reconciler (which records samples on every tick) and the
// UsageReport reconciler (which queries totals over a period).
//
// When a Store is attached, every sample is persisted and the Sampler hydrates
// its in-memory window from the store on startup, so history survives restarts.
// Without a Store, the Sampler keeps an in-memory window only.
type Sampler struct {
	retention time.Duration
	mu        sync.Mutex
	// samples indexed by CostProfile key ("namespace/name"), ordered by timestamp.
	samples map[string][]Sample
	now     func() time.Time // injectable for tests
	// store persists samples across restarts. When nil, the Sampler is
	// pure in-memory (the pre-persistence behaviour).
	store *Store
	// lastPrune tracks the last store prune so retention cleanup runs at
	// most every pruneInterval rather than on every Record.
	lastPrune time.Time
}

// Close closes the backing Store, if any, releasing the bbolt file lock. It is
// safe to call more than once and safe on a pure in-memory Sampler.
func (s *Sampler) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// NewSamplerWithStore returns a Sampler backed by a persistent Store. It
// hydrates the in-memory window from everything the store retains within the
// retention window, so a controller restart does not lose history. Returns an
// error only when hydration fails, in which case the caller should fail loud
// rather than silently start with an empty window.
func NewSamplerWithStore(retention time.Duration, store *Store) (*Sampler, error) {
	return newSamplerWithStore(retention, store, time.Now)
}

// newSamplerWithStore is the testable form of NewSamplerWithStore: it accepts
// the clock so hydration uses the same injected now() the tests already drive.
// A test that records samples against a fixedClock must hydrate against the
// same clock, or the retention window is computed on an unrelated wall clock.
func newSamplerWithStore(retention time.Duration, store *Store, now func() time.Time) (*Sampler, error) {
	s := &Sampler{
		retention: retention,
		samples:   make(map[string][]Sample),
		now:       now,
		store:     store,
	}
	if err := s.hydrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewSampler returns a Sampler with the default retention window and
// wall-clock time.
func NewSampler() *Sampler {
	return &Sampler{
		retention: DefaultRetention,
		samples:   make(map[string][]Sample),
		now:       time.Now,
	}
}

// NewSamplerWithRetention is exposed for tests that need a short window.
func NewSamplerWithRetention(retention time.Duration) *Sampler {
	s := NewSampler()
	s.retention = retention
	return s
}

// Record appends a sample for the given CostProfile key. Samples older than
// the retention window are GC'd on the same call to keep memory bounded.
// Concurrent callers are fine — the map is protected by mu.
//
// When a Store is attached, the sample is first persisted so it survives a
// restart, then appended to the in-memory window. A persistence failure is
// returned so the caller can surface it; the sample is not added to memory in
// that case, so a living window never diverges from what is durable.
func (s *Sampler) Record(key string, powerW, activeThresholdW float64) error {
	now := s.now()
	sample := Sample{
		At:      now,
		PowerW:  powerW,
		ActiveW: activeThresholdW,
	}

	if s.store != nil {
		if err := s.store.Append(key, sample); err != nil {
			return err
		}
		s.pruneStore(now)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples[key] = append(s.samples[key], sample)
	s.gcLocked(key, now)
	return nil
}

// WindowSummary describes the aggregated behavior of a CostProfile over a
// [start, end) window. Populated by (*Sampler).Summarize. All fields are
// zero when no samples overlap the window.
type WindowSummary struct {
	ActiveHours     float64 // wall-clock hours with power above idle threshold
	TotalHours      float64 // wall-clock hours actually observed (never the whole window if samples didn't span it)
	ActiveEnergyKWh float64 // integrated kilowatt-hours consumed during active intervals only
	TotalEnergyKWh  float64 // integrated kilowatt-hours consumed across every observed sample
}

// ActiveAndTotalHours is a convenience wrapper around Summarize that returns
// only the hours fields. Kept because UsageReport's utilization computation
// doesn't need energy, and this signature is easier to read at the callsite.
func (s *Sampler) ActiveAndTotalHours(key string, start, end time.Time) (float64, float64) {
	w := s.Summarize(key, start, end)
	return w.ActiveHours, w.TotalHours
}

// Summarize aggregates samples for the key over the [start, end) window
// and returns a WindowSummary with both time and energy totals.
//
// Integration rule: each sample represents its own state forward in time
// until the next sample (or `end`, whichever comes first). A sample is
// classified as "active" if its recorded PowerW exceeds the ActiveW
// threshold that was in effect when it was recorded. Energy is computed
// as rectangular integration: the sample's PowerW (W) is held flat across
// the interval it represents, so energy_kWh for that interval equals
// PowerW × duration_h / 1000. This is the same shape of math Prometheus
// uses for rate() windows — good enough for billing-accuracy when the
// sample spacing is small relative to the window (30s samples over a
// 24h window gives 2880 rectangles).
//
// Gaps before the first sample (time between `start` and samples[0]) are
// not credited — we treat them as "no data" rather than asserting an
// idle state that was never observed.
func (s *Sampler) Summarize(key string, start, end time.Time) WindowSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[key]
	if len(samples) == 0 {
		return WindowSummary{}
	}

	var summary WindowSummary
	for i, sample := range samples {
		// Interval this sample represents: [sample.At, nextAt).
		var nextAt time.Time
		if i+1 < len(samples) {
			nextAt = samples[i+1].At
		} else {
			nextAt = end
		}

		intervalStart := sample.At
		if intervalStart.Before(start) {
			intervalStart = start
		}
		intervalEnd := nextAt
		if intervalEnd.After(end) {
			intervalEnd = end
		}
		if !intervalEnd.After(intervalStart) {
			continue
		}
		secs := intervalEnd.Sub(intervalStart).Seconds()
		hours := secs / 3600.0
		energyKWh := sample.PowerW * hours / 1000.0

		summary.TotalHours += hours
		summary.TotalEnergyKWh += energyKWh
		if sample.PowerW > sample.ActiveW {
			summary.ActiveHours += hours
			summary.ActiveEnergyKWh += energyKWh
		}
	}

	return summary
}

// Snapshot returns a copy of the samples for `key` ordered by time. Intended
// for tests and debug endpoints.
func (s *Sampler) Snapshot(key string) []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.samples[key]
	dst := make([]Sample, len(src))
	copy(dst, src)
	return dst
}

// hydrate loads every retained sample from the store into the in-memory
// window. It is called once at construction. Samples are restored with their
// original classification (the ActiveW captured at record time), so a
// threshold change after a restart still respects the threshold in effect when
// each sample was recorded.
func (s *Sampler) hydrate() error {
	if s.store == nil {
		return nil
	}
	now := s.now()
	cutoff := now.Add(-s.retention)

	keys, err := s.store.Keys()
	if err != nil {
		return err
	}
	for _, key := range keys {
		samples, err := s.store.LoadRange(key, cutoff, now)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.samples[key] = samples
		s.mu.Unlock()
	}
	return nil
}

// pruneStore removes persisted samples older than the retention window. It
// runs at most every pruneInterval so the periodic full-scan delete does not
// fire on every Record (30s per CostProfile). The in-memory GC (gcLocked) is
// already correct on every Record; this keeps the durable store bounded too.
func (s *Sampler) pruneStore(now time.Time) {
	if s.store == nil {
		return
	}
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneInterval {
		return
	}
	s.lastPrune = now
	// Best-effort: a prune failure should not fail a reconcile; the store is
	// bounded by the next successful prune, and an over-retention bounded by
	// one pruneInterval is harmless for report accuracy.
	_ = s.store.Prune(now.Add(-s.retention))
}

// gcLocked drops samples older than the retention window. Caller must hold mu.
func (s *Sampler) gcLocked(key string, now time.Time) {
	cutoff := now.Add(-s.retention)
	samples := s.samples[key]
	// Fast path: nothing expired.
	if len(samples) == 0 || !samples[0].At.Before(cutoff) {
		return
	}
	// Find the first sample at or after cutoff.
	idx := sort.Search(len(samples), func(i int) bool {
		return !samples[i].At.Before(cutoff)
	})
	if idx >= len(samples) {
		s.samples[key] = samples[:0]
		return
	}
	trimmed := make([]Sample, len(samples)-idx)
	copy(trimmed, samples[idx:])
	s.samples[key] = trimmed
}

// DefaultIdleThresholdWatts computes a sensible default idle threshold when
// the CostProfile doesn't declare one. A safe rule of thumb: 20% of nameplate
// TDP across all GPUs in the profile. Falls back to 30 W × gpuCount when TDP
// isn't declared.
func DefaultIdleThresholdWatts(tdpPerGPU *int32, gpuCount int32) float64 {
	count := float64(gpuCount)
	if count <= 0 {
		count = 1
	}
	if tdpPerGPU != nil && *tdpPerGPU > 0 {
		return float64(*tdpPerGPU) * 0.2 * count
	}
	return 30.0 * count
}
