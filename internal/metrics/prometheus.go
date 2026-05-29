package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var GatewayActiveRequests = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_inflight_requests",
		Help: "Number of in-flight proxied requests per team",
	},
	[]string{"team_id"},
)

var GatewayTotalRequests = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gateway_total_requests",
		Help: "Number of total proxied requests per team",
	},
	[]string{"team_id"},
)

var GatewayRequestLatency = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gateway_request_latency",
		Help:    "Latency of proxied requests per team",
		Buckets: []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
	},
	[]string{"team_id"},
)

var GatewayRequestTTFT = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "gateway_request_ttft",
		Help:    "TTFT of proxied requests per team",
		Buckets: []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
	},
	[]string{"team_id"},
)
