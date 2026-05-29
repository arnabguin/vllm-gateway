package storage

import "time"

// RequestMetrics is one aggregated window of gateway metrics per team.
type RequestMetrics struct {
	Timestamp     time.Time
	TeamID        string
	TotalRequests uint64
	LatencyP50MS  uint32
	LatencyP95MS  uint32
	LatencyP99MS  uint32
	TTFTP50MS     uint32
	TTFTP95MS     uint32
	TTFTP99MS     uint32
}
