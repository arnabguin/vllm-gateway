package metrics

import (
	"math"
	"sync"

	"github.com/bmizerany/perks/quantile"
)

// StreamLatency maintains approximate percentiles in O(1) on each insert.
// Safe for one writer + concurrent readers (Insert uses Lock, Query uses RLock).
type StreamLatency struct {
	mu sync.RWMutex
	q  *quantile.Stream
}

func NewStreamLatency() *StreamLatency {
	return &StreamLatency{q: newQuantileStream()}
}

func newQuantileStream() *quantile.Stream {
	return quantile.NewTargeted(0.5, 0.95, 0.99)
}

func (s *StreamLatency) Insert(ms uint32) {
	s.mu.Lock()
	s.q.Insert(float64(ms))
	s.mu.Unlock()
}

// Percentile returns the current approximate percentile in milliseconds.
func (s *StreamLatency) Percentile(p float64) uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.q.Count() == 0 {
		return 0
	}
	return uint32(math.Round(s.q.Query(p)))
}

func (s *StreamLatency) P50() uint32 { return s.Percentile(0.5) }
func (s *StreamLatency) P95() uint32 { return s.Percentile(0.95) }
func (s *StreamLatency) P99() uint32 { return s.Percentile(0.99) }

func (s *StreamLatency) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.q.Count()
}

// Reset clears samples (e.g. after emitting a metrics window).
func (s *StreamLatency) Reset() {
	s.mu.Lock()
	s.q = newQuantileStream()
	s.mu.Unlock()
}
