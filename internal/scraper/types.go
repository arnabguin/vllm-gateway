package scraper

import (
	"time"
)

type VLLMSystemMetrics struct {
	Timestamp       time.Time
	QueueDepth      uint16 // waiting
	RunningRequests uint16
}
