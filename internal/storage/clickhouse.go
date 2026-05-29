package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/arnab-guin/vllm-gateway/internal/config"
	"github.com/arnab-guin/vllm-gateway/internal/scraper"
)

type ClickHouseStorage struct {
	conn     driver.Conn
	database string
}

var _ Storage = (*ClickHouseStorage)(nil)

func NewClickHouseStorage(cfg config.ClickHouseConfig) (*ClickHouseStorage, error) {
	// Connect to "default" for bootstrap (target DB may not exist yet).
	// Inserts use fully qualified table names so pooled connections never rely on USE.
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: cfg.Username,
			Password: cfg.Password,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	s := &ClickHouseStorage{conn: conn, database: cfg.Database}

	if err := waitForClickHouse(context.Background(), s.Ping); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	if err := EnsureRequestEventsTable(context.Background(), conn, cfg.Database); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := EnsureVLLMSystemMetricsTable(context.Background(), conn, cfg.Database); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := EnsureRequestMetricsTable(context.Background(), conn, cfg.Database); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

func waitForClickHouse(ctx context.Context, ping func(context.Context) error) error {
	const maxAttempts = 30
	backoff := time.Second
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == maxAttempts {
			break
		}
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff += time.Second
		}
	}
	return lastErr
}

func (s *ClickHouseStorage) Ping(ctx context.Context) error {
	return s.conn.Ping(ctx)
}

func (s *ClickHouseStorage) qualifiedTable(table string) string {
	return s.database + "." + table
}

func (s *ClickHouseStorage) InsertRequestEvent(ctx context.Context, e RequestEvent) error {
	query := fmt.Sprintf(`INSERT INTO %s (
		timestamp, team_id, project, user_id, model,
		prompt_tokens, completion_tokens, latency_ms, ttft_ms,
		status_code, error_message
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.qualifiedTable("request_events"))

	err := s.conn.Exec(ctx, query,
		e.Timestamp,
		e.TeamID,
		e.Project,
		e.UserID,
		e.Model,
		e.PromptTokens,
		e.CompletionTokens,
		e.LatencyMS,
		e.TTFTMS,
		e.StatusCode,
		e.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert request event: %w", err)
	}
	return nil
}

func (s *ClickHouseStorage) InsertRequestMetrics(ctx context.Context, m RequestMetrics) error {
	query := fmt.Sprintf(`INSERT INTO %s (
		timestamp, team_id, total_requests,
		latency_p50_ms, latency_p95_ms, latency_p99_ms,
		ttft_p50_ms, ttft_p95_ms, ttft_p99_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.qualifiedTable("request_metrics"))

	err := s.conn.Exec(ctx, query,
		m.Timestamp,
		m.TeamID,
		m.TotalRequests,
		m.LatencyP50MS,
		m.LatencyP95MS,
		m.LatencyP99MS,
		m.TTFTP50MS,
		m.TTFTP95MS,
		m.TTFTP99MS,
	)
	if err != nil {
		return fmt.Errorf("insert request metrics: %w", err)
	}
	return nil
}

func (s *ClickHouseStorage) InsertVLLMSystemMetrics(ctx context.Context, m scraper.VLLMSystemMetrics) error {
	query := fmt.Sprintf(`INSERT INTO %s (
		timestamp, queue_depth, running_requests,
		gpu_cache_usage_pct, ttft_p50_ms, ttft_p95_ms, tpot_p50_ms, tpot_p95_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, s.qualifiedTable("vllm_system_metrics"))

	err := s.conn.Exec(ctx, query,
		m.Timestamp,
		m.QueueDepth,
		m.RunningRequests,
		float32(0),
		float32(0),
		float32(0),
		float32(0),
		float32(0),
	)
	if err != nil {
		return fmt.Errorf("insert vllm system metrics: %w", err)
	}
	return nil
}
