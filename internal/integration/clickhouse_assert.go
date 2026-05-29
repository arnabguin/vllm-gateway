//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	e2eMockModel                = "mock-model"
	e2eMockPromptTokens         = uint32(12)
	e2eMockCompletionTokens     = uint32(7)
	e2eMockChatPromptTokens     = uint32(15)
	e2eMockChatCompletionTokens = uint32(8)
	e2eMockStatusCode           = uint16(200)
)

type chRequestEvent struct {
	TeamID           string
	Project          string
	UserID           string
	Model            string
	PromptTokens     uint32
	CompletionTokens uint32
	LatencyMS        uint32
	TTFTMS           uint32
	StatusCode       uint16
	ErrorMessage     string
}

type chRequestMetrics struct {
	TeamID        string
	TotalRequests uint64
	LatencyP50MS  uint32
	LatencyP95MS  uint32
	LatencyP99MS  uint32
	TTFTP50MS     uint32
	TTFTP95MS     uint32
	TTFTP99MS     uint32
}

func countRequestEvents(t *testing.T, conn driver.Conn, teamID string) uint64 {
	t.Helper()
	var n uint64
	if err := conn.QueryRow(context.Background(),
		"SELECT count() FROM request_events WHERE team_id = ?", teamID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countRequestMetricsRows(t *testing.T, conn driver.Conn, teamID string) uint64 {
	t.Helper()
	var n uint64
	if err := conn.QueryRow(context.Background(),
		"SELECT count() FROM request_metrics WHERE team_id = ?", teamID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sumPromptTokens(t *testing.T, conn driver.Conn, teamID string) uint64 {
	t.Helper()
	var n uint64
	if err := conn.QueryRow(context.Background(),
		"SELECT sum(prompt_tokens) FROM request_events WHERE team_id = ?", teamID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sumCompletionTokens(t *testing.T, conn driver.Conn, teamID string) uint64 {
	t.Helper()
	var n uint64
	if err := conn.QueryRow(context.Background(),
		"SELECT sum(completion_tokens) FROM request_events WHERE team_id = ?", teamID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fetchRequestEvents(t *testing.T, conn driver.Conn, teamID string) []chRequestEvent {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
		SELECT team_id, project, user_id, model,
		       prompt_tokens, completion_tokens, latency_ms, ttft_ms,
		       status_code, error_message
		FROM request_events
		WHERE team_id = ?
		ORDER BY timestamp
	`, teamID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []chRequestEvent
	for rows.Next() {
		var e chRequestEvent
		if err := rows.Scan(
			&e.TeamID, &e.Project, &e.UserID, &e.Model,
			&e.PromptTokens, &e.CompletionTokens, &e.LatencyMS, &e.TTFTMS,
			&e.StatusCode, &e.ErrorMessage,
		); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func fetchRequestMetricsRows(t *testing.T, conn driver.Conn, teamID string) []chRequestMetrics {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
		SELECT team_id, total_requests,
		       latency_p50_ms, latency_p95_ms, latency_p99_ms,
		       ttft_p50_ms, ttft_p95_ms, ttft_p99_ms
		FROM request_metrics
		WHERE team_id = ?
		ORDER BY timestamp
	`, teamID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []chRequestMetrics
	for rows.Next() {
		var m chRequestMetrics
		if err := rows.Scan(
			&m.TeamID, &m.TotalRequests,
			&m.LatencyP50MS, &m.LatencyP95MS, &m.LatencyP99MS,
			&m.TTFTP50MS, &m.TTFTP95MS, &m.TTFTP99MS,
		); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertClickHouseTeamExact(t *testing.T, conn driver.Conn, teamID string, wantRequests int) {
	t.Helper()
	assertClickHouseEventsExact(t, conn, teamID, wantRequests)
	assertClickHouseRequestMetricsExact(t, conn, teamID, wantRequests)
}

func assertClickHouseEventsExact(t *testing.T, conn driver.Conn, teamID string, wantRequests int) {
	t.Helper()

	eventRows := countRequestEvents(t, conn, teamID)
	if eventRows != uint64(wantRequests) {
		t.Fatalf("team %q request_events row count: got %d want %d", teamID, eventRows, wantRequests)
	}

	wantPromptSum := uint64(wantRequests) * uint64(e2eMockPromptTokens)
	if got := sumPromptTokens(t, conn, teamID); got != wantPromptSum {
		t.Fatalf("team %q sum(prompt_tokens): got %d want %d", teamID, got, wantPromptSum)
	}

	wantCompletionSum := uint64(wantRequests) * uint64(e2eMockCompletionTokens)
	if got := sumCompletionTokens(t, conn, teamID); got != wantCompletionSum {
		t.Fatalf("team %q sum(completion_tokens): got %d want %d", teamID, got, wantCompletionSum)
	}

	events := fetchRequestEvents(t, conn, teamID)
	assertRequestEventsExact(t, events, teamID, wantRequests)
}

func assertClickHouseRequestMetricsExact(t *testing.T, conn driver.Conn, teamID string, wantRequests int) {
	t.Helper()

	metricsRows := countRequestMetricsRows(t, conn, teamID)
	if metricsRows != 1 {
		t.Fatalf("team %q request_metrics row count: got %d want 1", teamID, metricsRows)
	}

	metrics := fetchRequestMetricsRows(t, conn, teamID)
	if len(metrics) != 1 {
		t.Fatalf("team %q request_metrics fetched rows: got %d want 1", teamID, len(metrics))
	}
	assertRequestMetricsExact(t, metrics[0], teamID, wantRequests)
}

func assertRequestEventsExact(t *testing.T, events []chRequestEvent, teamID string, wantCount int) {
	t.Helper()
	if len(events) != wantCount {
		t.Fatalf("team %q request_events rows: got %d want %d", teamID, len(events), wantCount)
	}
	for i, e := range events {
		prefix := fmt.Sprintf("team %q request_events[%d]", teamID, i)
		if e.TeamID != teamID {
			t.Fatalf("%s team_id: got %q want %q", prefix, e.TeamID, teamID)
		}
		if e.Project != "" {
			t.Fatalf("%s project: got %q want empty", prefix, e.Project)
		}
		if e.UserID != "" {
			t.Fatalf("%s user_id: got %q want empty", prefix, e.UserID)
		}
		if e.Model != e2eMockModel {
			t.Fatalf("%s model: got %q want %q", prefix, e.Model, e2eMockModel)
		}
		if e.PromptTokens != e2eMockPromptTokens {
			t.Fatalf("%s prompt_tokens: got %d want %d", prefix, e.PromptTokens, e2eMockPromptTokens)
		}
		if e.CompletionTokens != e2eMockCompletionTokens {
			t.Fatalf("%s completion_tokens: got %d want %d", prefix, e.CompletionTokens, e2eMockCompletionTokens)
		}
		if e.LatencyMS == 0 {
			t.Fatalf("%s latency_ms: got 0 want > 0", prefix)
		}
		if e.TTFTMS != 0 {
			t.Fatalf("%s ttft_ms: got %d want 0", prefix, e.TTFTMS)
		}
		if e.StatusCode != e2eMockStatusCode {
			t.Fatalf("%s status_code: got %d want %d", prefix, e.StatusCode, e2eMockStatusCode)
		}
		if e.ErrorMessage != "" {
			t.Fatalf("%s error_message: got %q want empty", prefix, e.ErrorMessage)
		}
	}
}

func assertRequestMetricsExact(t *testing.T, m chRequestMetrics, teamID string, wantCount int) {
	t.Helper()
	prefix := fmt.Sprintf("team %q request_metrics", teamID)
	if m.TeamID != teamID {
		t.Fatalf("%s team_id: got %q want %q", prefix, m.TeamID, teamID)
	}
	if m.TotalRequests != uint64(wantCount) {
		t.Fatalf("%s total_requests: got %d want %d", prefix, m.TotalRequests, wantCount)
	}
	if m.LatencyP50MS == 0 || m.LatencyP95MS == 0 || m.LatencyP99MS == 0 {
		t.Fatalf("%s latency percentiles must be non-zero: p50=%d p95=%d p99=%d",
			prefix, m.LatencyP50MS, m.LatencyP95MS, m.LatencyP99MS)
	}
	if m.LatencyP50MS > m.LatencyP95MS || m.LatencyP95MS > m.LatencyP99MS {
		t.Fatalf("%s latency percentiles out of order: p50=%d p95=%d p99=%d",
			prefix, m.LatencyP50MS, m.LatencyP95MS, m.LatencyP99MS)
	}
	if m.TTFTP50MS != 0 || m.TTFTP95MS != 0 || m.TTFTP99MS != 0 {
		t.Fatalf("%s ttft percentiles must be zero: p50=%d p95=%d p99=%d",
			prefix, m.TTFTP50MS, m.TTFTP95MS, m.TTFTP99MS)
	}
}

func assertStreamChatRequestEventsExact(t *testing.T, events []chRequestEvent, teamID string, wantCount int) {
	t.Helper()
	assertStreamRequestEventsExact(t, events, teamID, wantCount, e2eMockChatPromptTokens, e2eMockChatCompletionTokens)
}

func assertStreamCompletionsRequestEventsExact(t *testing.T, events []chRequestEvent, teamID string, wantCount int) {
	t.Helper()
	assertStreamRequestEventsExact(t, events, teamID, wantCount, e2eMockPromptTokens, e2eMockCompletionTokens)
}

func assertStreamRequestEventsExact(t *testing.T, events []chRequestEvent, teamID string, wantCount int, wantPrompt, wantCompletion uint32) {
	t.Helper()
	if len(events) != wantCount {
		t.Fatalf("team %q request_events rows: got %d want %d", teamID, len(events), wantCount)
	}
	for i, e := range events {
		prefix := fmt.Sprintf("team %q stream request_events[%d]", teamID, i)
		if e.TeamID != teamID {
			t.Fatalf("%s team_id: got %q want %q", prefix, e.TeamID, teamID)
		}
		if e.Model != e2eMockModel {
			t.Fatalf("%s model: got %q want %q", prefix, e.Model, e2eMockModel)
		}
		if e.PromptTokens != wantPrompt {
			t.Fatalf("%s prompt_tokens: got %d want %d", prefix, e.PromptTokens, wantPrompt)
		}
		if e.CompletionTokens != wantCompletion {
			t.Fatalf("%s completion_tokens: got %d want %d", prefix, e.CompletionTokens, wantCompletion)
		}
		if e.LatencyMS == 0 {
			t.Fatalf("%s latency_ms: got 0 want > 0", prefix)
		}
		if e.TTFTMS == 0 {
			t.Fatalf("%s ttft_ms: got 0 want > 0", prefix)
		}
		if e.StatusCode != e2eMockStatusCode {
			t.Fatalf("%s status_code: got %d want %d", prefix, e.StatusCode, e2eMockStatusCode)
		}
	}
}

func assertStreamRequestMetricsExact(t *testing.T, m chRequestMetrics, teamID string, wantCount int) {
	t.Helper()
	prefix := fmt.Sprintf("team %q stream request_metrics", teamID)
	if m.TeamID != teamID {
		t.Fatalf("%s team_id: got %q want %q", prefix, m.TeamID, teamID)
	}
	if m.TotalRequests != uint64(wantCount) {
		t.Fatalf("%s total_requests: got %d want %d", prefix, m.TotalRequests, wantCount)
	}
	if m.LatencyP50MS == 0 || m.LatencyP95MS == 0 || m.LatencyP99MS == 0 {
		t.Fatalf("%s latency percentiles must be non-zero: p50=%d p95=%d p99=%d",
			prefix, m.LatencyP50MS, m.LatencyP95MS, m.LatencyP99MS)
	}
	if m.TTFTP50MS == 0 || m.TTFTP95MS == 0 || m.TTFTP99MS == 0 {
		t.Fatalf("%s ttft percentiles must be non-zero: p50=%d p95=%d p99=%d",
			prefix, m.TTFTP50MS, m.TTFTP95MS, m.TTFTP99MS)
	}
}
