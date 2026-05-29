package scraper

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// gaugeMetrics are vLLM Prometheus gauge metric families (MVP).
var gaugeMetrics = []string{
	"vllm:num_requests_running",
	"vllm:num_requests_waiting",
}

// SystemMetricsSink persists scraped vLLM system metrics (e.g. ClickHouse).
type SystemMetricsSink interface {
	InsertVLLMSystemMetrics(ctx context.Context, m VLLMSystemMetrics) error
}

type VLLMScraper struct {
	MetricsURL string
	Interval   time.Duration
	Client     *http.Client
}

func NewVLLMScraper(vllmBaseURL string, interval time.Duration, client *http.Client) *VLLMScraper {
	metricsURL := strings.TrimRight(vllmBaseURL, "/") + "/metrics"
	return &VLLMScraper{
		MetricsURL: metricsURL,
		Interval:   interval,
		Client:     client,
	}
}

func (s *VLLMScraper) scrapeOnce(ctx context.Context) (VLLMSystemMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.MetricsURL, nil)
	if err != nil {
		return VLLMSystemMetrics{}, fmt.Errorf("create metrics request: %w", err)
	}

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return VLLMSystemMetrics{}, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return VLLMSystemMetrics{}, fmt.Errorf("metrics status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return VLLMSystemMetrics{}, fmt.Errorf("read metrics body: %w", err)
	}

	running, err := ParseGauge(body, gaugeMetrics[0])
	if err != nil {
		return VLLMSystemMetrics{}, fmt.Errorf("parse %s: %w", gaugeMetrics[0], err)
	}
	waiting, err := ParseGauge(body, gaugeMetrics[1])
	if err != nil {
		return VLLMSystemMetrics{}, fmt.Errorf("parse %s: %w", gaugeMetrics[1], err)
	}

	return VLLMSystemMetrics{
		Timestamp:       time.Now(),
		RunningRequests: uint16(running),
		QueueDepth:      uint16(waiting),
	}, nil
}

// Start runs scrape+insert on a ticker until ctx is cancelled.
func (s *VLLMScraper) Start(ctx context.Context, sink SystemMetricsSink) {
	go func() {
		ticker := time.NewTicker(s.Interval)
		defer ticker.Stop()

		run := func() {
			scrapeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			m, err := s.scrapeOnce(scrapeCtx)
			if err != nil {
				log.Printf("vllm metrics scrape: %v", err)
				return
			}
			if err := sink.InsertVLLMSystemMetrics(scrapeCtx, m); err != nil {
				log.Printf("vllm metrics insert: %v", err)
			}
		}

		run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
