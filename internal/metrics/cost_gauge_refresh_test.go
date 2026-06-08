package metrics

import (
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/arnab-guin/vllm-gateway/internal/cost"
)

const testGPUUSDPerHour = 2.50

func TestRefreshCostGaugesTeamTokensAndCost(t *testing.T) {
	team := "refresh-team-a"
	tp := cost.NewTokenThroughput()
	tp.RecordRequest(team, 100, 50)

	calc := cost.NewCalculator(testGPUUSDPerHour, tp)
	refresher := NewCostGaugeRefresher()
	t.Cleanup(func() { cleanupCostGaugeTeam(team) })

	refresher.Refresh(calc, tp)

	if got := gaugeVecValue(t, GatewayTeamPromptTokens10m, team); got != 100 {
		t.Fatalf("prompt tokens 10m = %v, want 100", got)
	}
	if got := gaugeVecValue(t, GatewayTeamCompletionTokens10m, team); got != 50 {
		t.Fatalf("completion tokens 10m = %v, want 50", got)
	}
	if got := gaugeVecValue(t, GatewayTeamTokens10m, team); got != 150 {
		t.Fatalf("total tokens 10m = %v, want 150", got)
	}

	usdPerToken := testGPUUSDPerHour / (150.0 * 6.0)
	assertFloatClose(t, "cluster usd per million", scalarGaugeValue(t, GatewayClusterUSDPerMillionTokens), usdPerToken*1_000_000)
	assertFloatClose(t, "team cost 10m", gaugeVecValue(t, GatewayTeamEstimatedCostUSD10m, team), 150*usdPerToken)
	assertFloatClose(t, "team cost per hour", gaugeVecValue(t, GatewayTeamEstimatedCostUSDPerHour, team), 150*6*usdPerToken)
}

func TestRefreshCostGaugesNoTraffic(t *testing.T) {
	tp := cost.NewTokenThroughput()
	calc := cost.NewCalculator(testGPUUSDPerHour, tp)
	refresher := NewCostGaugeRefresher()

	refresher.Refresh(calc, tp)

	if got := scalarGaugeValue(t, GatewayClusterUSDPerMillionTokens); got != 0 {
		t.Fatalf("cluster usd per million = %v, want 0", got)
	}
}

func TestRefreshCostGaugesZeroGPUCost(t *testing.T) {
	team := "refresh-team-zero-gpu"
	tp := cost.NewTokenThroughput()
	tp.RecordRequest(team, 10, 5)

	calc := cost.NewCalculator(0, tp)
	refresher := NewCostGaugeRefresher()
	t.Cleanup(func() { cleanupCostGaugeTeam(team) })

	refresher.Refresh(calc, tp)

	if got := gaugeVecValue(t, GatewayTeamTokens10m, team); got != 15 {
		t.Fatalf("total tokens 10m = %v, want 15", got)
	}
	if got := gaugeVecValue(t, GatewayTeamEstimatedCostUSD10m, team); got != 0 {
		t.Fatalf("team cost 10m = %v, want 0", got)
	}
	if got := scalarGaugeValue(t, GatewayClusterUSDPerMillionTokens); got != 0 {
		t.Fatalf("cluster usd per million = %v, want 0", got)
	}
}

func TestRefreshCostGaugesStaleTeamCleanup(t *testing.T) {
	team := "refresh-team-stale"
	tp := cost.NewTokenThroughput()
	tp.RecordRequest(team, 1, 1)

	calc := cost.NewCalculator(testGPUUSDPerHour, tp)
	refresher := NewCostGaugeRefresher()
	t.Cleanup(func() { cleanupCostGaugeTeam(team) })

	refresher.Refresh(calc, tp)
	if !gatherHasTeamGauge(t, "gateway_team_tokens_10m", team) {
		t.Fatal("expected team gauge before reset")
	}

	tp.ResetWindow(team)
	refresher.Refresh(calc, tp)
	if gatherHasTeamGauge(t, "gateway_team_tokens_10m", team) {
		t.Fatal("expected stale team gauge to be deleted")
	}
}

func gatherHasTeamGauge(t *testing.T, metricName, teamID string) bool {
	t.Helper()
	fams, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, fam := range fams {
		if fam.GetName() != metricName {
			continue
		}
		for _, m := range fam.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "team_id" && lp.GetValue() == teamID {
					return true
				}
			}
		}
	}
	return false
}

func gaugeVecValue(t *testing.T, vec *prometheus.GaugeVec, teamID string) float64 {
	t.Helper()
	m, err := vec.GetMetricWithLabelValues(teamID)
	if err != nil {
		t.Fatalf("gauge missing label team_id=%q: %v", teamID, err)
	}
	return writeGaugeValue(t, m)
}

func scalarGaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	return writeGaugeValue(t, g)
}

func writeGaugeValue(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		t.Fatal(err)
	}
	return d.GetGauge().GetValue()
}

func assertFloatClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: got %v want %v", name, got, want)
	}
}

func cleanupCostGaugeTeam(teamID string) {
	GatewayTeamPromptTokens10m.DeleteLabelValues(teamID)
	GatewayTeamCompletionTokens10m.DeleteLabelValues(teamID)
	GatewayTeamTokens10m.DeleteLabelValues(teamID)
	GatewayTeamEstimatedCostUSD10m.DeleteLabelValues(teamID)
	GatewayTeamEstimatedCostUSDPerHour.DeleteLabelValues(teamID)
}
