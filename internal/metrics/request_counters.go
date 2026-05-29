package metrics

import (
	"sync"
	"sync/atomic"
)

// Thread-safe counters
// ActiveCounter tracks "currently active" counts per teamID.
// TotalCounter tracks "total" counts per teamID.
type ActiveCounter struct {
	teams sync.Map // teamId -> *atomic.Uint64
}
type TotalCounter struct {
	teams sync.Map // teamId -> *atomic.Uint64
}

type RequestCounters struct {
	Active *ActiveCounter
	Total  *TotalCounter
}

func NewRequestCounters() *RequestCounters {
	return &RequestCounters{
		Active: &ActiveCounter{},
		Total:  &TotalCounter{},
	}
}

func (ac *ActiveCounter) Inc(teamID string) uint64 {
	n := ac.teamCounter(teamID).Add(1)
	GatewayActiveRequests.WithLabelValues(teamID).Set(float64(n))
	return n
}

// Dec decrements a team's counter, clamping at 0.
func (ac *ActiveCounter) Dec(teamID string) uint64 {
	c, ok := ac.teams.Load(teamID)
	if !ok {
		return 0
	}
	n := decClamp0(c.(*atomic.Uint64))
	GatewayActiveRequests.WithLabelValues(teamID).Set(float64(n))
	return n
}

func (ac *ActiveCounter) Get(teamID string) uint64 {
	if v, ok := ac.teams.Load(teamID); ok {
		return v.(*atomic.Uint64).Load()
	}
	return 0
}

func (ac *ActiveCounter) ResetTeam(teamID string) {
	ac.teams.Delete(teamID)
}

func (ac *ActiveCounter) teamCounter(teamID string) *atomic.Uint64 {
	if v, ok := ac.teams.Load(teamID); ok {
		return v.(*atomic.Uint64)
	}
	c := new(atomic.Uint64)
	actual, _ := ac.teams.LoadOrStore(teamID, c)
	return actual.(*atomic.Uint64)
}

func (tc *TotalCounter) Inc(teamID string) uint64 {
	n := tc.teamCounter(teamID).Add(1)
	GatewayTotalRequests.WithLabelValues(teamID).Inc()
	return n
}

func (tc *TotalCounter) Get(teamID string) uint64 {
	if v, ok := tc.teams.Load(teamID); ok {
		return v.(*atomic.Uint64).Load()
	}
	return 0
}

func (tc *TotalCounter) ResetTeam(teamID string) {
	tc.teams.Delete(teamID)
	GatewayTotalRequests.DeleteLabelValues(teamID)
}

// ResetWindow clears the in-memory per-window count without touching Prometheus.
func (tc *TotalCounter) ResetWindow(teamID string) {
	tc.teams.Delete(teamID)
}

// ForEachTeam calls fn for every team with a non-zero window counter.
func (tc *TotalCounter) ForEachTeam(fn func(teamID string)) {
	tc.teams.Range(func(key, _ any) bool {
		fn(key.(string))
		return true
	})
}

func (tc *TotalCounter) teamCounter(teamID string) *atomic.Uint64 {
	if v, ok := tc.teams.Load(teamID); ok {
		return v.(*atomic.Uint64)
	}
	c := new(atomic.Uint64)
	actual, _ := tc.teams.LoadOrStore(teamID, c)
	return actual.(*atomic.Uint64)
}

func decClamp0(c *atomic.Uint64) uint64 {
	for {
		cur := c.Load()
		if cur == 0 {
			return 0
		}
		if c.CompareAndSwap(cur, cur-1) {
			return cur - 1
		}
	}
}
