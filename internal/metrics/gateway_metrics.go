package metrics

import "context"

// GatewayMetrics holds per-request instrumentation shared by the proxy and emit loop.
type GatewayMetrics struct {
	Counters *RequestCounters
	Latency  *LatencyRegistry
	TTFT     *LatencyRegistry
}

func NewGatewayMetrics() *GatewayMetrics {
	return &GatewayMetrics{
		Counters: NewRequestCounters(),
		Latency:  NewLatencyRegistry(0, observerRequestLatency),
		TTFT:     NewLatencyRegistry(0, observerRequestTTFT),
	}
}

func observerRequestLatency(teamID string, ms uint32) {
	GatewayRequestLatency.WithLabelValues(teamID).Observe(float64(ms))
}

func observerRequestTTFT(teamID string, ms uint32) {
	GatewayRequestTTFT.WithLabelValues(teamID).Observe(float64(ms))
}

// Start runs background ingest goroutines for latency registries.
func (g *GatewayMetrics) Start(ctx context.Context) {
	if g.Latency != nil {
		g.Latency.Start(ctx)
	}
	if g.TTFT != nil {
		g.TTFT.Start(ctx)
	}
}
