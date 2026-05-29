package metrics

import (
	"context"
	"log"
	"time"

	"github.com/arnab-guin/vllm-gateway/internal/storage"
)

const defaultEmitInterval = 15 * time.Second

// RequestMetricsEmitter periodically flushes per-team aggregates to ClickHouse.
type RequestMetricsEmitter struct {
	store    storage.Storage
	interval time.Duration
	metrics  *GatewayMetrics
}

func NewRequestMetricsEmitter(store storage.Storage, interval time.Duration, m *GatewayMetrics) *RequestMetricsEmitter {
	if interval <= 0 {
		interval = defaultEmitInterval
	}
	return &RequestMetricsEmitter{
		store:    store,
		interval: interval,
		metrics:  m,
	}
}

// Start runs the emit loop until ctx is cancelled.
func (e *RequestMetricsEmitter) Start(ctx context.Context) {
	if e.store == nil || e.metrics == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.emit(ctx)
			}
		}
	}()
}

// EmitOnce flushes current per-team aggregates immediately (used by tests and manual triggers).
func (e *RequestMetricsEmitter) EmitOnce(ctx context.Context) {
	if e.store == nil || e.metrics == nil {
		return
	}
	e.emit(ctx)
}

func (e *RequestMetricsEmitter) emit(ctx context.Context) {
	ts := time.Now()
	for teamID := range e.collectTeams() {
		total := e.metrics.Counters.Total.Get(teamID)
		latN := e.metrics.Latency.Count(teamID)
		ttftN := e.metrics.TTFT.Count(teamID)
		if total == 0 && latN == 0 && ttftN == 0 {
			continue
		}

		row := storage.RequestMetrics{
			Timestamp:     ts,
			TeamID:        teamID,
			TotalRequests: total,
			LatencyP50MS:  e.metrics.Latency.P50(teamID),
			LatencyP95MS:  e.metrics.Latency.P95(teamID),
			LatencyP99MS:  e.metrics.Latency.P99(teamID),
			TTFTP50MS:     e.metrics.TTFT.P50(teamID),
			TTFTP95MS:     e.metrics.TTFT.P95(teamID),
			TTFTP99MS:     e.metrics.TTFT.P99(teamID),
		}
		if err := e.store.InsertRequestMetrics(ctx, row); err != nil {
			log.Printf("insert request_metrics team=%s: %v", teamID, err)
			continue
		}

		e.metrics.Counters.Total.ResetWindow(teamID)
		e.metrics.Latency.ResetTeam(teamID)
		e.metrics.TTFT.ResetTeam(teamID)
	}
}

func (e *RequestMetricsEmitter) collectTeams() map[string]struct{} {
	teams := make(map[string]struct{})
	if e.metrics.Counters != nil && e.metrics.Counters.Total != nil {
		e.metrics.Counters.Total.ForEachTeam(func(teamID string) {
			teams[teamID] = struct{}{}
		})
	}
	if e.metrics.Latency != nil {
		e.metrics.Latency.ForEachTeam(func(teamID string) {
			teams[teamID] = struct{}{}
		})
	}
	if e.metrics.TTFT != nil {
		e.metrics.TTFT.ForEachTeam(func(teamID string) {
			teams[teamID] = struct{}{}
		})
	}
	return teams
}
