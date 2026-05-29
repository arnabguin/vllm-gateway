package metrics

import (
	"context"
	"sync"
	"sync/atomic"
)

const defaultLatencyIngestBuffer = 4096

type latencySample struct {
	teamID string
	ms     uint32
}

// SampleObserver is called from the ingest goroutine after each sample is recorded.
type SampleObserver func(teamID string, ms uint32)

// LatencyRegistry ingests latency samples on a dedicated goroutine so the HTTP
// hot path never takes a lock. Percentiles are updated on each insert and can
// be read at any time (approximate streaming quantiles).
type LatencyRegistry struct {
	ch       chan latencySample
	teams    sync.Map // teamID -> *StreamLatency
	drops    atomic.Uint64
	observer SampleObserver
}

func NewLatencyRegistry(ingestBuffer int, observer SampleObserver) *LatencyRegistry {
	if ingestBuffer <= 0 {
		ingestBuffer = defaultLatencyIngestBuffer
	}
	return &LatencyRegistry{
		ch:       make(chan latencySample, ingestBuffer),
		observer: observer,
	}
}

// Start runs the ingest loop until ctx is cancelled.
func (r *LatencyRegistry) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case s := <-r.ch:
				r.teamStream(s.teamID).Insert(s.ms)
				if r.observer != nil {
					r.observer(s.teamID, s.ms)
				}
			}
		}
	}()
}

// Record enqueues a sample without locking. Drops if the buffer is full.
func (r *LatencyRegistry) Record(teamID string, ms uint32) {
	select {
	case r.ch <- latencySample{teamID: teamID, ms: ms}:
	default:
		r.drops.Add(1)
	}
}

func (r *LatencyRegistry) Drops() uint64 {
	return r.drops.Load()
}

func (r *LatencyRegistry) P50(teamID string) uint32 {
	return r.teamStream(teamID).P50()
}

func (r *LatencyRegistry) P95(teamID string) uint32 {
	return r.teamStream(teamID).P95()
}

func (r *LatencyRegistry) P99(teamID string) uint32 {
	return r.teamStream(teamID).P99()
}

func (r *LatencyRegistry) Percentile(teamID string, p float64) uint32 {
	return r.teamStream(teamID).Percentile(p)
}

func (r *LatencyRegistry) Count(teamID string) int {
	if v, ok := r.teams.Load(teamID); ok {
		return v.(*StreamLatency).Count()
	}
	return 0
}

// ResetTeam clears a team's latency distribution after a metrics emit window.
func (r *LatencyRegistry) ResetTeam(teamID string) {
	if v, ok := r.teams.Load(teamID); ok {
		v.(*StreamLatency).Reset()
	}
}

// ForEachTeam calls fn for every team that has recorded at least one sample.
func (r *LatencyRegistry) ForEachTeam(fn func(teamID string)) {
	r.teams.Range(func(key, _ any) bool {
		fn(key.(string))
		return true
	})
}

func (r *LatencyRegistry) teamStream(teamID string) *StreamLatency {
	if v, ok := r.teams.Load(teamID); ok {
		return v.(*StreamLatency)
	}
	s := NewStreamLatency()
	actual, _ := r.teams.LoadOrStore(teamID, s)
	return actual.(*StreamLatency)
}
