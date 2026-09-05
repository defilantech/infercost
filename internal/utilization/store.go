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
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"go.etcd.io/bbolt"
)

// Store persists power samples to a bbolt database so history survives
// controller restarts. It is the durable backing for the in-memory Sampler:
// the Sampler keeps a current window for fast queries, and the Store is
// rehydrated on startup and written to on each Record.
//
// A single bbolt file holds one bucket per CostProfile key
// ("namespace/name"). Within a bucket, keys are the sample timestamps encoded
// as big-endian nanoseconds, so bucket iteration is chronological. The value
// holds the power draw and the idle threshold in effect when it was recorded,
// so a threshold change never retroactively relabels old samples.
//
// This is a local state file, not an external database to host: the operator
// owns it, and there is no network dependency at report time.
type Store struct {
	db *bbolt.DB
}

// sampleKeyLen is the encoded byte length of a timestamp-ordered bucket key.
const sampleKeyLen = 8

// sampleValueLen is the encoded byte length of a sample value (two float64s).
const sampleValueLen = 16

// OpenStore opens (or creates) the bbolt database at path. It returns an error
// if the database is locked by another process after the timeout, so a
// single-replica operator never hangs waiting on a stale lock.
func OpenStore(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("opening sample store: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database. Safe to call once at shutdown.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Append writes one sample for the given profile key. It creates the bucket
// on first use. Timestamps are stored in UTC nanoseconds; a sample recorded at
// the same nanosecond as an existing one overwrites it, which cannot happen at
// the 30s sampling cadence.
func (s *Store) Append(key string, sample Sample) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(key))
		if err != nil {
			return err
		}
		return b.Put(mkKey(sample.At), encodeSample(sample))
	})
}

// LoadRange returns the samples for a key with timestamps in [start, end),
// in ascending order. end may be zero to mean "no upper bound".
func (s *Store) LoadRange(key string, start, end time.Time) ([]Sample, error) {
	var out []Sample
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(key))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		startKey := mkKey(start)
		for k, v := c.Seek(startKey); k != nil; k, v = c.Next() {
			if end.IsZero() {
				// no upper bound
			} else if bytes.Compare(k, mkKey(end)) >= 0 {
				break
			}
			sample, err := decodeSample(k, v)
			if err != nil {
				return err
			}
			out = append(out, sample)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Keys returns every profile key that has samples in the store.
func (s *Store) Keys() ([]string, error) {
	var keys []string
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			keys = append(keys, string(name))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// Prune removes every sample recorded at or before cutoff across all keys.
// It collects keys to delete first and then deletes them, because bbolt does
// not allow a cursor to seek and delete in the same pass.
func (s *Store) Prune(cutoff time.Time) error {
	cutoffKey := mkKey(cutoff)
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(_ []byte, b *bbolt.Bucket) error {
			var stale [][]byte
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if bytes.Compare(k, cutoffKey) >= 0 {
					break
				}
				// Copy because the cursor reuses the key slice on Next.
				stale = append(stale, append([]byte(nil), k...))
			}
			for _, k := range stale {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func mkKey(t time.Time) []byte {
	b := make([]byte, sampleKeyLen)
	binary.BigEndian.PutUint64(b, uint64(t.UTC().UnixNano()))
	return b
}

func decodeSampleKey(k []byte) time.Time {
	nano := int64(binary.BigEndian.Uint64(k))
	return time.Unix(0, nano).UTC()
}

func encodeSample(s Sample) []byte {
	b := make([]byte, sampleValueLen)
	binary.BigEndian.PutUint64(b[0:8], math.Float64bits(s.PowerW))
	binary.BigEndian.PutUint64(b[8:16], math.Float64bits(s.ActiveW))
	return b
}

func decodeSample(k, v []byte) (Sample, error) {
	if len(v) != sampleValueLen {
		return Sample{}, fmt.Errorf("invalid sample value length %d", len(v))
	}
	return Sample{
		At:      decodeSampleKey(k),
		PowerW:  math.Float64frombits(binary.BigEndian.Uint64(v[0:8])),
		ActiveW: math.Float64frombits(binary.BigEndian.Uint64(v[8:16])),
	}, nil
}
