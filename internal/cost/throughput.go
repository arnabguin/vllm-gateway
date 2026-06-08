package cost

import (
	"sync"
	"time"
)

const ThroughputWindow = 10 * time.Minute
const ThroughputBucket = 1 * time.Minute
const ThroughputSlots = 10

type TokenKind int

const (
	TokenPrompt TokenKind = iota
	TokenCompletion
)

type TokenThroughput struct {
	teamsPrompt     sync.Map // teamID -> *teamTokenWindow
	teamsCompletion sync.Map // teamID -> *teamTokenWindow
}

type teamTokenWindow struct {
	mu    sync.Mutex
	slots [ThroughputSlots]uint64
	times [ThroughputSlots]time.Time
}

func NewTokenThroughput() *TokenThroughput {
	return &TokenThroughput{}
}

func slotActive(slotTime time.Time, now time.Time) bool {
	if slotTime.IsZero() {
		return false
	}
	return !slotTime.Before(now.Add(-ThroughputWindow))
}

func tokensPerHour(tokensInWindow uint64) float64 {
	if tokensInWindow == 0 || ThroughputWindow <= 0 {
		return 0
	}
	return float64(tokensInWindow) * (60.0 / ThroughputWindow.Minutes())
}

func (w *teamTokenWindow) record(tokens uint64, now time.Time) {
	bucket := now.Truncate(ThroughputBucket)
	slot := int(bucket.Unix()/60) % ThroughputSlots

	if w.times[slot].Truncate(ThroughputBucket) != bucket {
		w.slots[slot] = 0
		w.times[slot] = bucket
	}
	w.slots[slot] += tokens
}

func (w *teamTokenWindow) sumActive(now time.Time) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	var total uint64
	for i, slot := range w.slots {
		if slotActive(w.times[i], now) {
			total += slot
		}
	}
	return total
}

func (t *TokenThroughput) RecordRequest(teamID string, promptTokens, completionTokens uint32) {
	now := time.Now()
	if promptTokens > 0 {
		t.recordTokenAt(TokenPrompt, teamID, uint64(promptTokens), now)
	}
	if completionTokens > 0 {
		t.recordTokenAt(TokenCompletion, teamID, uint64(completionTokens), now)
	}
}

func (t *TokenThroughput) RecordPrompt(teamID string, promptTokens uint32) {
	t.RecordToken(TokenPrompt, teamID, promptTokens)
}

func (t *TokenThroughput) RecordCompletion(teamID string, completionTokens uint32) {
	t.RecordToken(TokenCompletion, teamID, completionTokens)
}

func (t *TokenThroughput) RecordToken(kind TokenKind, teamID string, tokens uint32) {
	if tokens == 0 {
		return
	}
	t.recordTokenAt(kind, teamID, uint64(tokens), time.Now())
}

func (t *TokenThroughput) recordTokenAt(kind TokenKind, teamID string, tokens uint64, now time.Time) {
	teams := t.teamsFor(kind)
	if teams == nil {
		return
	}

	v, _ := teams.LoadOrStore(teamID, &teamTokenWindow{})
	window := v.(*teamTokenWindow)

	window.mu.Lock()
	defer window.mu.Unlock()
	window.record(tokens, now)
}

func (t *TokenThroughput) teamsFor(kind TokenKind) *sync.Map {
	switch kind {
	case TokenPrompt:
		return &t.teamsPrompt
	case TokenCompletion:
		return &t.teamsCompletion
	default:
		return nil
	}
}

func (t *TokenThroughput) TotalRequestTokens(teamID string) uint64 {
	return t.TotalTokens(TokenPrompt, teamID) + t.TotalTokens(TokenCompletion, teamID)
}

func (t *TokenThroughput) ClusterTotalRequestTokens() uint64 {
	return t.ClusterTotalTokens(TokenPrompt) + t.ClusterTotalTokens(TokenCompletion)
}

func (t *TokenThroughput) TokensPerHour(kind TokenKind, teamID string) float64 {
	return tokensPerHour(t.TotalTokens(kind, teamID))
}

func (t *TokenThroughput) TotalRequestTokensPerHour(teamID string) float64 {
	return tokensPerHour(t.TotalRequestTokens(teamID))
}

func (t *TokenThroughput) ClusterTokensPerHour(kind TokenKind) float64 {
	return tokensPerHour(t.ClusterTotalTokens(kind))
}

func (t *TokenThroughput) ClusterRequestTokensPerHour() float64 {
	return tokensPerHour(t.ClusterTotalRequestTokens())
}

func (t *TokenThroughput) ForEachTeam(kind TokenKind, fn func(teamID string)) {
	teams := t.teamsFor(kind)
	if teams == nil {
		return
	}
	teams.Range(func(key, _ any) bool {
		fn(key.(string))
		return true
	})
}

// ForEachRequestTeam calls fn once per team with prompt or completion tokens in the window.
func (t *TokenThroughput) ForEachRequestTeam(fn func(teamID string)) {
	seen := make(map[string]struct{})
	t.ForEachTeam(TokenPrompt, func(teamID string) {
		if _, ok := seen[teamID]; ok {
			return
		}
		seen[teamID] = struct{}{}
		fn(teamID)
	})
	t.ForEachTeam(TokenCompletion, func(teamID string) {
		if _, ok := seen[teamID]; ok {
			return
		}
		seen[teamID] = struct{}{}
		fn(teamID)
	})
}

func (t *TokenThroughput) ResetWindow(teamID string) {
	t.teamsPrompt.Delete(teamID)
	t.teamsCompletion.Delete(teamID)
}

func (t *TokenThroughput) ResetCluster() {
	t.teamsPrompt.Range(func(key, _ any) bool {
		t.teamsPrompt.Delete(key)
		return true
	})
	t.teamsCompletion.Range(func(key, _ any) bool {
		t.teamsCompletion.Delete(key)
		return true
	})
}

func (t *TokenThroughput) ResetTeam(kind TokenKind, teamID string) {
	teams := t.teamsFor(kind)
	if teams == nil {
		return
	}
	teams.Delete(teamID)
}

func (t *TokenThroughput) TotalTokens(kind TokenKind, teamID string) uint64 {
	teams := t.teamsFor(kind)
	if teams == nil {
		return 0
	}

	v, ok := teams.Load(teamID)
	if !ok {
		return 0
	}
	return v.(*teamTokenWindow).sumActive(time.Now())
}

func (t *TokenThroughput) ClusterTotalTokens(kind TokenKind) uint64 {
	teams := t.teamsFor(kind)
	if teams == nil {
		return 0
	}

	now := time.Now()
	var total uint64
	teams.Range(func(_, value any) bool {
		total += value.(*teamTokenWindow).sumActive(now)
		return true
	})
	return total
}
