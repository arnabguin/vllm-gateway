package cost

import (
	"math"
	"testing"
)

const testGPUUSDPerHour = 2.50

func newTestCalculator(t *testing.T) (*Calculator, *TokenThroughput) {
	t.Helper()
	tp := NewTokenThroughput()
	return NewCalculator(testGPUUSDPerHour, tp), tp
}

func TestNewCalculatorNilThroughputPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewCalculator(nil throughput): expected panic")
		}
	}()
	NewCalculator(testGPUUSDPerHour, nil)
}

func TestClusterTokensPerHourColdStart(t *testing.T) {
	t.Parallel()
	calc, _ := newTestCalculator(t)

	got, ok := calc.ClusterTokensPerHour()
	if ok {
		t.Fatalf("ClusterTokensPerHour() ok = true, want false (no traffic)")
	}
	if got != 0 {
		t.Fatalf("ClusterTokensPerHour() = %v, want 0", got)
	}
}

func TestUSDPerTokenHappyPath(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 100_000, 0)

	got, ok := calc.USDPerToken()
	if !ok {
		t.Fatal("USDPerToken(): ok = false, want true")
	}

	want := testGPUUSDPerHour / 600_000.0 // 100k tokens in 10m → 600k/hr
	if got != want {
		t.Fatalf("USDPerToken() = %v, want %v", got, want)
	}
}

func TestUSDPerMillionTokens(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 100_000, 0)

	got, ok := calc.USDPerMillionTokens()
	if !ok {
		t.Fatal("USDPerMillionTokens(): ok = false, want true")
	}

	want := testGPUUSDPerHour / 600_000.0 * 1_000_000
	if got != want {
		t.Fatalf("USDPerMillionTokens() = %v, want %v", got, want)
	}
}

func TestClusterCostPerHour(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)

	got, ok := calc.ClusterCostPerHour()
	if ok {
		t.Fatal("ClusterCostPerHour() with no traffic: ok = true, want false")
	}

	tp.RecordRequest("team-a", 1_000, 0)
	got, ok = calc.ClusterCostPerHour()
	if !ok {
		t.Fatal("ClusterCostPerHour() with traffic: ok = false, want true")
	}
	if got != testGPUUSDPerHour {
		t.Fatalf("ClusterCostPerHour() = %v, want %v", got, testGPUUSDPerHour)
	}
}

func TestClusterCostPerHourZeroGPU(t *testing.T) {
	t.Parallel()
	tp := NewTokenThroughput()
	tp.RecordRequest("team-a", 1_000, 0)
	calc := NewCalculator(0, tp)

	got, ok := calc.ClusterCostPerHour()
	if ok {
		t.Fatal("ClusterCostPerHour() with zero GPU rate: ok = true, want false")
	}
	if got != 0 {
		t.Fatalf("ClusterCostPerHour() = %v, want 0", got)
	}
}

func TestRequestCostColdStart(t *testing.T) {
	t.Parallel()
	calc, _ := newTestCalculator(t)

	got, ok := calc.RequestCost(12, 7)
	if ok {
		t.Fatalf("RequestCost() before traffic: ok = true, want false (got %v)", got)
	}
	if got != 0 {
		t.Fatalf("RequestCost() = %v, want 0", got)
	}
}

func TestRequestCostZeroTokens(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 100, 0)

	got, ok := calc.RequestCost(0, 0)
	if ok {
		t.Fatal("RequestCost(0,0): ok = true, want false")
	}
	if got != 0 {
		t.Fatalf("RequestCost(0,0) = %v, want 0", got)
	}
}

func TestRequestCostAfterRecord(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 100_000, 0)

	usdPerToken, ok := calc.USDPerToken()
	if !ok {
		t.Fatal("USDPerToken(): ok = false")
	}

	got, ok := calc.RequestCost(12, 7)
	if !ok {
		t.Fatal("RequestCost(): ok = false, want true")
	}

	want := 19 * usdPerToken
	if got != want {
		t.Fatalf("RequestCost(12,7) = %v, want %v", got, want)
	}
}

func TestRequestCostIncludesCurrentWindowTraffic(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 100_000, 0)

	// Same request tokens included in throughput before costing (handler order).
	tp.RecordRequest("team-b", 12, 7)
	got, ok := calc.RequestCost(12, 7)
	if !ok {
		t.Fatal("RequestCost(): ok = false, want true")
	}
	if got <= 0 {
		t.Fatalf("RequestCost() = %v, want > 0", got)
	}
}

func TestTeamTokensPerHourAndShare(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 10_000, 0)
	tp.RecordRequest("team-b", 20_000, 0)

	teamRate, ok := calc.TeamTokensPerHour("team-a")
	if !ok {
		t.Fatal("TeamTokensPerHour(team-a): ok = false")
	}
	if teamRate != 60_000.0 {
		t.Fatalf("TeamTokensPerHour(team-a) = %v, want 60000", teamRate)
	}

	share, ok := calc.TeamTokenShare("team-a")
	if !ok {
		t.Fatal("TeamTokenShare(team-a): ok = false")
	}
	wantShare := 60_000.0 / 180_000.0
	if math.Abs(share-wantShare) > 1e-9 {
		t.Fatalf("TeamTokenShare(team-a) = %v, want %v", share, wantShare)
	}
}

func TestTeamTokenShareUnknownTeam(t *testing.T) {
	t.Parallel()
	calc, tp := newTestCalculator(t)
	tp.RecordRequest("team-a", 10_000, 0)

	_, ok := calc.TeamTokenShare("missing")
	if ok {
		t.Fatal("TeamTokenShare(missing): ok = true, want false")
	}
}
