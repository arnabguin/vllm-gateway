package storage

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const RequestEventsDDL = `
CREATE TABLE IF NOT EXISTS request_events (
    timestamp DateTime64(3),
    team_id String,
    project String,
    user_id String,
    model String,
    prompt_tokens UInt32,
    completion_tokens UInt32,
    latency_ms UInt32,
    ttft_ms UInt32,
    status_code UInt16,
    error_message String
) ENGINE = MergeTree()
ORDER BY (timestamp, team_id)
`
const VLLMSystemMetricsDDL = `
CREATE TABLE IF NOT EXISTS vllm_system_metrics (
    timestamp DateTime,
    queue_depth UInt16,
    running_requests UInt16,
    gpu_cache_usage_pct Float32,
    ttft_p50_ms Float32,
    ttft_p95_ms Float32,
    tpot_p50_ms Float32,
    tpot_p95_ms Float32
) ENGINE = MergeTree()
ORDER BY timestamp
`

const RequestMetricsDDL = `
CREATE TABLE IF NOT EXISTS request_metrics (
    timestamp DateTime64(3),
    team_id String,
    total_requests UInt64,
    latency_p50_ms UInt32,
    latency_p95_ms UInt32,
    latency_p99_ms UInt32,
    ttft_p50_ms UInt32,
    ttft_p95_ms UInt32,
    ttft_p99_ms UInt32
) ENGINE = MergeTree()
ORDER BY (timestamp, team_id)
`

// EnsureRequestEventsTable creates the database and request_events table.
// database must come from trusted config (not user input).
func EnsureRequestEventsTable(ctx context.Context, conn driver.Conn, database string) error {
	if err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", database)); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if err := conn.Exec(ctx, fmt.Sprintf("USE %s", database)); err != nil {
		return fmt.Errorf("use database: %w", err)
	}
	if err := conn.Exec(ctx, RequestEventsDDL); err != nil {
		return fmt.Errorf("create request_events: %w", err)
	}
	return nil
}

// EnsureRequestMetricsTable creates the database and request_metrics table.
func EnsureRequestMetricsTable(ctx context.Context, conn driver.Conn, database string) error {
	if err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", database)); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if err := conn.Exec(ctx, fmt.Sprintf("USE %s", database)); err != nil {
		return fmt.Errorf("use database: %w", err)
	}
	if err := conn.Exec(ctx, RequestMetricsDDL); err != nil {
		return fmt.Errorf("create request_metrics: %w", err)
	}
	return nil
}

// EnsureVLLMSystemMetricsTable creates the database and metrics table.
// database must come from trusted config (not user input).
func EnsureVLLMSystemMetricsTable(ctx context.Context, conn driver.Conn, database string) error {
	if err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", database)); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if err := conn.Exec(ctx, fmt.Sprintf("USE %s", database)); err != nil {
		return fmt.Errorf("use database: %w", err)
	}
	if err := conn.Exec(ctx, VLLMSystemMetricsDDL); err != nil {
		return fmt.Errorf("create vllm_system_metrics: %w", err)
	}
	return nil
}
