//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func parsePrometheusText(body []byte) (map[string]*dto.MetricFamily, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse prometheus text: %w", err)
	}
	return families, nil
}

func labelValue(pairs []*dto.LabelPair, name string) string {
	for _, lp := range pairs {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}

func labeledGaugeSeries(families map[string]*dto.MetricFamily, name string) (map[string]float64, error) {
	mf, ok := families[name]
	if !ok || mf == nil {
		return nil, fmt.Errorf("metric %q not found", name)
	}
	if mf.GetType() != dto.MetricType_GAUGE {
		return nil, fmt.Errorf("metric %q is not a gauge", name)
	}
	out := make(map[string]float64)
	for _, m := range mf.GetMetric() {
		teamID := labelValue(m.GetLabel(), "team_id")
		if teamID == "" {
			continue
		}
		if m.GetGauge() == nil {
			return nil, fmt.Errorf("metric %q team %q has no gauge value", name, teamID)
		}
		out[teamID] = m.GetGauge().GetValue()
	}
	return out, nil
}

func labeledCounterSeries(families map[string]*dto.MetricFamily, name string) (map[string]float64, error) {
	mf, ok := families[name]
	if !ok || mf == nil {
		return nil, fmt.Errorf("metric %q not found", name)
	}
	if mf.GetType() != dto.MetricType_COUNTER {
		return nil, fmt.Errorf("metric %q is not a counter", name)
	}
	out := make(map[string]float64)
	for _, m := range mf.GetMetric() {
		teamID := labelValue(m.GetLabel(), "team_id")
		if teamID == "" {
			continue
		}
		if m.GetCounter() == nil {
			return nil, fmt.Errorf("metric %q team %q has no counter value", name, teamID)
		}
		out[teamID] = m.GetCounter().GetValue()
	}
	return out, nil
}

type histogramStats struct {
	Count uint64
	Sum   float64
}

func labeledHistogramSeries(families map[string]*dto.MetricFamily, name string) (map[string]histogramStats, error) {
	mf, ok := families[name]
	if !ok || mf == nil {
		return nil, fmt.Errorf("metric %q not found", name)
	}
	if mf.GetType() != dto.MetricType_HISTOGRAM {
		return nil, fmt.Errorf("metric %q is not a histogram", name)
	}
	out := make(map[string]histogramStats)
	for _, m := range mf.GetMetric() {
		teamID := labelValue(m.GetLabel(), "team_id")
		if teamID == "" {
			continue
		}
		if m.GetHistogram() == nil {
			return nil, fmt.Errorf("metric %q team %q has no histogram value", name, teamID)
		}
		h := m.GetHistogram()
		out[teamID] = histogramStats{
			Count: h.GetSampleCount(),
			Sum:   h.GetSampleSum(),
		}
	}
	return out, nil
}

func teamSet(teams []string) map[string]struct{} {
	out := make(map[string]struct{}, len(teams))
	for _, team := range teams {
		out[team] = struct{}{}
	}
	return out
}

func assertSeriesMapExact(
	t *testing.T,
	metricName string,
	got map[string]float64,
	wantTeams map[string]struct{},
	wantValue func(teamID string) float64,
) {
	t.Helper()
	if len(got) != len(wantTeams) {
		t.Fatalf("%s time series count: got %d want %d (series=%v)", metricName, len(got), len(wantTeams), got)
	}
	for team := range wantTeams {
		v, ok := got[team]
		if !ok {
			t.Fatalf("%s missing time series for team %q", metricName, team)
		}
		want := wantValue(team)
		if v != want {
			t.Fatalf("%s team %q value: got %v want %v", metricName, team, v, want)
		}
	}
	for team, v := range got {
		if _, ok := wantTeams[team]; !ok {
			t.Fatalf("%s unexpected time series team %q value %v", metricName, team, v)
		}
	}
}

func assertHistogramSeriesMapExact(
	t *testing.T,
	metricName string,
	got map[string]histogramStats,
	wantTeams map[string]struct{},
	wantCount func(teamID string) uint64,
) {
	t.Helper()
	if len(got) != len(wantTeams) {
		t.Fatalf("%s time series count: got %d want %d (series=%v)", metricName, len(got), len(wantTeams), got)
	}
	for team := range wantTeams {
		h, ok := got[team]
		if !ok {
			t.Fatalf("%s missing time series for team %q", metricName, team)
		}
		want := wantCount(team)
		if h.Count != want {
			t.Fatalf("%s team %q sample_count: got %d want %d", metricName, team, h.Count, want)
		}
		if h.Sum <= 0 {
			t.Fatalf("%s team %q sample_sum: got %v want > 0", metricName, team, h.Sum)
		}
	}
	for team, h := range got {
		if _, ok := wantTeams[team]; !ok {
			t.Fatalf("%s unexpected time series team %q count=%d sum=%v", metricName, team, h.Count, h.Sum)
		}
	}
}

func assertPrometheusExact(t *testing.T, families map[string]*dto.MetricFamily, teams []string, requestsPerTeam int) {
	t.Helper()
	wantTeams := teamSet(teams)

	inflight, err := labeledGaugeSeries(families, "gateway_inflight_requests")
	if err != nil {
		t.Fatal(err)
	}
	assertSeriesMapExact(t, "gateway_inflight_requests", inflight, wantTeams, func(string) float64 { return 0 })

	totals, err := labeledCounterSeries(families, "gateway_total_requests")
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := float64(requestsPerTeam)
	assertSeriesMapExact(t, "gateway_total_requests", totals, wantTeams, func(string) float64 { return wantTotal })

	histograms, err := labeledHistogramSeries(families, "gateway_request_latency")
	if err != nil {
		t.Fatal(err)
	}
	wantHistCount := func(string) uint64 { return uint64(requestsPerTeam) }
	assertHistogramSeriesMapExact(t, "gateway_request_latency", histograms, wantTeams, wantHistCount)
}
