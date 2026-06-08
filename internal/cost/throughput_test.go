package cost

import (
	"sync"
	"testing"
	"time"
)

func TestTotalTokensUnknownTeam(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	if got := tp.TotalTokens(TokenPrompt, "missing"); got != 0 {
		t.Fatalf("TotalTokens(missing) = %d, want 0", got)
	}
}

func TestRecordTokenZeroNoOp(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordToken(TokenPrompt, "team-a", 0)
	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != 0 {
		t.Fatalf("zero record created entry: got %d", got)
	}
}

func TestRecordAndTotalTokens(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 12, 7)
	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != 12 {
		t.Fatalf("prompt tokens = %d, want 12", got)
	}
	if got := tp.TotalTokens(TokenCompletion, "team-a"); got != 7 {
		t.Fatalf("completion tokens = %d, want 7", got)
	}
	if got := tp.TotalRequestTokens("team-a"); got != 19 {
		t.Fatalf("total request tokens = %d, want 19", got)
	}
}

func TestClusterTotals(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 10, 5)
	tp.RecordRequest("team-b", 20, 15)

	if got := tp.ClusterTotalTokens(TokenPrompt); got != 30 {
		t.Fatalf("cluster prompt = %d, want 30", got)
	}
	if got := tp.ClusterTotalTokens(TokenCompletion); got != 20 {
		t.Fatalf("cluster completion = %d, want 20", got)
	}
	if got := tp.ClusterTotalRequestTokens(); got != 50 {
		t.Fatalf("cluster total = %d, want 50", got)
	}
}

func TestTokensPerHourExtrapolation(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 100_000, 0)

	got := tp.ClusterRequestTokensPerHour()
	want := 600_000.0
	if got != want {
		t.Fatalf("ClusterRequestTokensPerHour() = %v, want %v", got, want)
	}
}

func TestSlotExpiry(t *testing.T) {
	w := &teamTokenWindow{}
	now := time.Date(2026, 6, 2, 12, 7, 30, 0, time.UTC)

	w.mu.Lock()
	w.record(100, now.Add(-11*time.Minute))
	w.record(50, now)
	w.mu.Unlock()

	if got := w.sumActive(now); got != 50 {
		t.Fatalf("sumActive after expiry = %d, want 50 (stale bucket dropped)", got)
	}
}

func TestResetTeamAndCluster(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 10, 5)
	tp.RecordRequest("team-b", 20, 15)

	tp.ResetTeam(TokenPrompt, "team-a")
	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != 0 {
		t.Fatalf("after ResetTeam prompt, team-a = %d, want 0", got)
	}
	if got := tp.TotalTokens(TokenCompletion, "team-a"); got != 5 {
		t.Fatalf("ResetTeam(prompt) cleared completion map entry unexpectedly: got %d", got)
	}

	tp.ResetCluster()
	if got := tp.ClusterTotalRequestTokens(); got != 0 {
		t.Fatalf("after ResetCluster = %d, want 0", got)
	}
}

func TestResetWindowClearsBothKinds(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 10, 5)
	tp.ResetWindow("team-a")
	if got := tp.TotalRequestTokens("team-a"); got != 0 {
		t.Fatalf("ResetWindow total = %d, want 0", got)
	}
}

func TestForEachTeam(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 1, 0)
	tp.RecordRequest("team-b", 2, 0)

	seen := make(map[string]struct{})
	tp.ForEachTeam(TokenPrompt, func(teamID string) {
		seen[teamID] = struct{}{}
	})
	if len(seen) != 2 {
		t.Fatalf("ForEachTeam saw %d teams, want 2", len(seen))
	}
}

func TestForEachRequestTeam(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordPrompt("team-a", 1)
	tp.RecordCompletion("team-b", 2)

	var calls int
	seen := make(map[string]struct{})
	tp.ForEachRequestTeam(func(teamID string) {
		calls++
		if _, ok := seen[teamID]; ok {
			t.Fatalf("ForEachRequestTeam called twice for %q", teamID)
		}
		seen[teamID] = struct{}{}
	})
	if calls != 2 || len(seen) != 2 {
		t.Fatalf("ForEachRequestTeam saw %d teams, want 2", len(seen))
	}
}

func TestRecordPrompt(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordPrompt("team-a", 42)

	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != 42 {
		t.Fatalf("prompt tokens = %d, want 42", got)
	}
	if got := tp.TotalTokens(TokenCompletion, "team-a"); got != 0 {
		t.Fatalf("completion tokens = %d, want 0", got)
	}
}

func TestRecordCompletion(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordCompletion("team-a", 17)

	if got := tp.TotalTokens(TokenCompletion, "team-a"); got != 17 {
		t.Fatalf("completion tokens = %d, want 17", got)
	}
	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != 0 {
		t.Fatalf("prompt tokens = %d, want 0", got)
	}
}

func TestTokensPerHour(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordPrompt("team-a", 10_000)

	got := tp.TokensPerHour(TokenPrompt, "team-a")
	want := 60_000.0 // 10k in 10m → 60k/hr
	if got != want {
		t.Fatalf("TokensPerHour() = %v, want %v", got, want)
	}
}

func TestTotalRequestTokensPerHour(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 10_000, 5_000)

	got := tp.TotalRequestTokensPerHour("team-a")
	want := 90_000.0 // 15k in 10m → 90k/hr
	if got != want {
		t.Fatalf("TotalRequestTokensPerHour() = %v, want %v", got, want)
	}
}

func TestClusterTokensPerHour(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordPrompt("team-a", 10_000)
	tp.RecordPrompt("team-b", 5_000)
	tp.RecordCompletion("team-a", 2_000)

	if got := tp.ClusterTokensPerHour(TokenPrompt); got != 90_000.0 {
		t.Fatalf("ClusterTokensPerHour(prompt) = %v, want 90000", got)
	}
	if got := tp.ClusterTokensPerHour(TokenCompletion); got != 12_000.0 {
		t.Fatalf("ClusterTokensPerHour(completion) = %v, want 12000", got)
	}
}

func TestRecordTokenConcurrent(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	const (
		goroutines = 20
		each       = 100
	)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tp.RecordToken(TokenPrompt, "team-a", each)
		}()
	}
	wg.Wait()

	want := uint64(goroutines * each)
	if got := tp.TotalTokens(TokenPrompt, "team-a"); got != want {
		t.Fatalf("concurrent total = %d, want %d", got, want)
	}
}
