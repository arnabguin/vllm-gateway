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

// Rolling 10-minute token window (updated at Prometheus scrape time, not per request).
var GatewayTeamTokens10m = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_team_tokens_10m",
		Help: "Prompt plus completion tokens per team in the rolling 10-minute window",
	},
	[]string{"team_id"},
)

var GatewayTeamPromptTokens10m = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_team_prompt_tokens_10m",
		Help: "Prompt tokens per team in the rolling 10-minute window",
	},
	[]string{"team_id"},
)

var GatewayTeamCompletionTokens10m = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_team_completion_tokens_10m",
		Help: "Completion tokens per team in the rolling 10-minute window",
	},
	[]string{"team_id"},
)

// Team-attributed cost derived from cluster USD/token and team token usage.
var GatewayTeamEstimatedCostUSD10m = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_team_estimated_cost_usd_10m",
		Help: "Estimated USD attributed to the team for tokens consumed in the rolling 10-minute window",
	},
	[]string{"team_id"},
)

var GatewayTeamEstimatedCostUSDPerHour = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gateway_team_estimated_cost_usd_per_hour",
		Help: "Estimated USD per hour for the team, extrapolated from the rolling 10-minute token window",
	},
	[]string{"team_id"},
)

// Cluster-level effective price (same for all teams; not labeled by team_id).
var GatewayClusterUSDPerMillionTokens = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "gateway_cluster_usd_per_million_tokens",
		Help: "Effective USD per million tokens cluster-wide, from gpu_usd_per_hour and measured cluster throughput",
	},
)
