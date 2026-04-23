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

// DefaultRetention is how far back the Sampler keeps samples. 48h is enough
// for daily and weekly-so-far reports; monthly reports fall back to a linear
// extrapolation of what's retained.
const DefaultRetention = 48 * time.Hour

// Sample is a single power-draw observation for a CostProfile.
type Sample struct {
	At      time.Time
	PowerW  float64
	ActiveW float64 // threshold in effect at record time — used so threshold changes don't retroactively relabel old samples
}

// Sampler is concurrency-safe. A single Sampler instance is shared between the
// CostProfile reconciler (which records samples on every tick) and the
// UsageReport reconciler (which queries totals over a period).
type Sampler struct {
	retention time.Duration
	mu        sync.Mutex
	// samples indexed by CostProfile key ("namespace/name"), ordered by timestamp.
	samples map[string][]Sample
	now     func() time.Time // injectable for tests
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
func (s *Sampler) Record(key string, powerW, activeThresholdW float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.samples[key] = append(s.samples[key], Sample{
		At:      now,
		PowerW:  powerW,
		ActiveW: activeThresholdW,
	})
	s.gcLocked(key, now)
}

// ActiveAndTotalHours returns (activeHours, totalHours) observed for the key
// between start (inclusive) and end (exclusive). Windows with no samples
// return (0, 0) — callers should treat that as "unknown" rather than "idle"
// to avoid falsely reporting 0% utilization during bootstrap.
//
// Integration rule: each sample represents its own state forward in time
// until the next sample (or `end`, whichever comes first). A sample is
// classified as "active" if its recorded PowerW exceeds the ActiveW
// threshold that was in effect when it was recorded. Persisting the
// threshold on each sample means a mid-period threshold change doesn't
// retroactively relabel old samples.
//
// Gaps before the first sample (time between `start` and samples[0]) are
// not credited — we treat them as "no data" rather than asserting an
// idle state that was never observed.
func (s *Sampler) ActiveAndTotalHours(key string, start, end time.Time) (float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[key]
	if len(samples) == 0 {
		return 0, 0
	}

	var activeSecs, totalSecs float64
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
		totalSecs += secs
		if sample.PowerW > sample.ActiveW {
			activeSecs += secs
		}
	}

	return activeSecs / 3600.0, totalSecs / 3600.0
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
