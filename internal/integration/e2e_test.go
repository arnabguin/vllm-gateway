//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Fprintln(os.Stderr, "docker not found; skipping integration tests")
		os.Exit(0)
	}

	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := compose(root, "down", "-v", "--remove-orphans"); err != nil {
		fmt.Fprintln(os.Stderr, "compose down:", err)
		os.Exit(1)
	}

	upCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := composeWithContext(upCtx, root, "up", "-d", "--wait", "--build"); err != nil {
		fmt.Fprintln(os.Stderr, "compose up:", err)
		_ = compose(root, "down", "-v", "--remove-orphans")
		os.Exit(1)
	}

	code := m.Run()

	if err := compose(root, "down", "-v", "--remove-orphans"); err != nil {
		fmt.Fprintln(os.Stderr, "compose down:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func TestGatewayE2E(t *testing.T) {
	runID := time.Now().UnixNano()
	teamA := fmt.Sprintf("e2e-a-%d", runID)
	teamB := fmt.Sprintf("e2e-b-%d", runID)
	teams := []string{teamA, teamB}
	const requestsPerTeam = 5

	waitForHTTP200(t, e2eGatewayURL+"/health", 30*time.Second)
	postCompletionsConcurrent(t, e2eGatewayURL, teams, requestsPerTeam)

	conn := openClickHouse(t)
	defer conn.Close()

	for _, teamID := range teams {
		waitForEventCountExact(t, conn, teamID, requestsPerTeam, 30*time.Second)
		assertClickHouseEventsExact(t, conn, teamID, requestsPerTeam)
	}
	postDebugEmitRequestMetrics(t, e2eGatewayURL)
	for _, teamID := range teams {
		assertClickHouseRequestMetricsExact(t, conn, teamID, requestsPerTeam)
	}
	waitForInflightGaugeZero(t, e2eGatewayURL+"/v1/metrics", teams, 5*time.Second)

	metricsBody := fetchPrometheusText(t, e2eGatewayURL+"/v1/metrics")
	families, err := parsePrometheusText(metricsBody)
	if err != nil {
		t.Fatal(err)
	}
	assertPrometheusExact(t, families, teams, requestsPerTeam)
}

// TestRequestMetricsEmitTiming checks request_metrics rollup via manual emit only
// (request_events are covered by TestGatewayE2E).
func TestRequestMetricsEmitTiming(t *testing.T) {
	runID := time.Now().UnixNano()
	teamID := fmt.Sprintf("e2e-emit-%d", runID)
	const requestsPerTeam = 5

	waitForHTTP200(t, e2eGatewayURL+"/health", 30*time.Second)
	postCompletionsConcurrent(t, e2eGatewayURL, []string{teamID}, requestsPerTeam)

	conn := openClickHouse(t)
	defer conn.Close()

	if n := countRequestMetricsRows(t, conn, teamID); n != 0 {
		t.Fatalf("request_metrics before manual emit: got %d want 0", n)
	}

	postDebugEmitRequestMetrics(t, e2eGatewayURL)
	assertClickHouseRequestMetricsExact(t, conn, teamID, requestsPerTeam)
}

func TestGatewayStreamingE2E(t *testing.T) {
	runID := time.Now().UnixNano()
	teamChatA := fmt.Sprintf("e2e-stream-chat-a-%d", runID)
	teamChatB := fmt.Sprintf("e2e-stream-chat-b-%d", runID)
	teamCompletions := fmt.Sprintf("e2e-stream-cmpl-%d", runID)
	chatTeams := []string{teamChatA, teamChatB}
	allTeams := append(append([]string{}, chatTeams...), teamCompletions)
	const requestsPerChatTeam = 5

	waitForHTTP200(t, e2eGatewayURL+"/health", 30*time.Second)

	// Concurrent chat streams (multiple requests, multiple teams).
	postStreamsChatConcurrent(t, e2eGatewayURL, chatTeams, requestsPerChatTeam)

	// Legacy completions streaming + SSE payload check.
	cmplPayloads, err := postStreamCompletionErr(e2eGatewayURL, teamCompletions)
	if err != nil {
		t.Fatal(err)
	}
	assertCompletionsStreamPayloads(t, cmplPayloads)

	waitForInflightGaugeZero(t, e2eGatewayURL+"/v1/metrics", allTeams, 5*time.Second)

	conn := openClickHouse(t)
	defer conn.Close()

	for _, teamID := range chatTeams {
		waitForEventCountExact(t, conn, teamID, requestsPerChatTeam, 30*time.Second)
		events := fetchRequestEvents(t, conn, teamID)
		assertStreamChatRequestEventsExact(t, events, teamID, requestsPerChatTeam)
	}
	waitForEventCountExact(t, conn, teamCompletions, 1, 30*time.Second)
	cmplEvents := fetchRequestEvents(t, conn, teamCompletions)
	assertStreamCompletionsRequestEventsExact(t, cmplEvents, teamCompletions, 1)

	postDebugEmitRequestMetrics(t, e2eGatewayURL)
	for _, teamID := range chatTeams {
		metrics := fetchRequestMetricsRows(t, conn, teamID)
		if len(metrics) != 1 {
			t.Fatalf("team %q request_metrics rows: got %d want 1", teamID, len(metrics))
		}
		assertStreamRequestMetricsExact(t, metrics[0], teamID, requestsPerChatTeam)
	}
	cmplMetrics := fetchRequestMetricsRows(t, conn, teamCompletions)
	if len(cmplMetrics) != 1 {
		t.Fatalf("team %q request_metrics rows: got %d want 1", teamCompletions, len(cmplMetrics))
	}
	assertStreamRequestMetricsExact(t, cmplMetrics[0], teamCompletions, 1)

	metricsBody := fetchPrometheusText(t, e2eGatewayURL+"/v1/metrics")
	families, err := parsePrometheusText(metricsBody)
	if err != nil {
		t.Fatal(err)
	}
	ttftHist, err := labeledHistogramSeries(families, "gateway_request_ttft")
	if err != nil {
		t.Fatal(err)
	}
	for _, teamID := range allTeams {
		h, ok := ttftHist[teamID]
		if !ok {
			t.Fatalf("gateway_request_ttft missing series for team %q", teamID)
		}
		if h.Count < 1 {
			t.Fatalf("gateway_request_ttft sample_count for %q: got %d want >= 1", teamID, h.Count)
		}
		if h.Sum <= 0 {
			t.Fatalf("gateway_request_ttft sample_sum for %q: got %v want > 0", teamID, h.Sum)
		}
	}
}

func TestGatewayEmbeddingsE2E(t *testing.T) {
	runID := time.Now().UnixNano()
	teamID := fmt.Sprintf("e2e-embed-%d", runID)
	const requests = 1

	waitForHTTP200(t, e2eGatewayURL+"/health", 30*time.Second)
	if err := postEmbeddingErr(e2eGatewayURL, teamID); err != nil {
		t.Fatal(err)
	}

	conn := openClickHouse(t)
	defer conn.Close()

	waitForEventCountExact(t, conn, teamID, requests, 30*time.Second)
	events := fetchRequestEvents(t, conn, teamID)
	assertEmbeddingRequestEventsExact(t, events, teamID, requests)

	waitForInflightGaugeZero(t, e2eGatewayURL+"/v1/metrics", []string{teamID}, 5*time.Second)
}

func waitForEventCountExact(t *testing.T, conn driver.Conn, teamID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastCount uint64
	for time.Now().Before(deadline) {
		lastCount = countRequestEvents(t, conn, teamID)
		if lastCount == uint64(want) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for exactly %d request_events for team %q: got %d", want, teamID, lastCount)
}

func postDebugEmitRequestMetrics(t *testing.T, gatewayURL string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/debug/emit-request-metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e2eHTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("debug emit status=%d body=%s", resp.StatusCode, body)
	}
}

func fetchPrometheusText(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("prometheus scrape status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func waitForInflightGaugeZero(t *testing.T, metricsURL string, teams []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		metricsBody := fetchPrometheusText(t, metricsURL)
		families, err := parsePrometheusText(metricsBody)
		if err != nil {
			t.Fatal(err)
		}
		inflight, err := labeledGaugeSeries(families, "gateway_inflight_requests")
		if err != nil {
			t.Fatal(err)
		}
		allZero := true
		for _, teamID := range teams {
			if inflight[teamID] != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	metricsBody := fetchPrometheusText(t, metricsURL)
	families, err := parsePrometheusText(metricsBody)
	if err != nil {
		t.Fatal(err)
	}
	inflight, err := labeledGaugeSeries(families, "gateway_inflight_requests")
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timeout waiting for gateway_inflight_requests to reach zero for teams %v; last series=%v", teams, inflight)
}

// postCompletionsConcurrent fires requestsPerTeam concurrent requests for each team,
// with all teams running at the same time.
func postCompletionsConcurrent(t *testing.T, gatewayURL string, teams []string, requestsPerTeam int) {
	t.Helper()

	var wg sync.WaitGroup
	errCh := make(chan error, len(teams)*requestsPerTeam)

	for _, teamID := range teams {
		for range requestsPerTeam {
			wg.Add(1)
			go func(team string) {
				defer wg.Done()
				if err := postCompletionErr(gatewayURL, team); err != nil {
					errCh <- fmt.Errorf("team %q: %w", team, err)
				}
			}(teamID)
		}
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../..")), nil
}

func compose(root string, args ...string) error {
	return composeWithContext(context.Background(), root, args...)
}

func composeWithContext(ctx context.Context, root string, args ...string) error {
	cmdArgs := append([]string{
		"compose",
		"-f", "docker-compose.integration.yml",
		"-p", "vllm-gateway-e2e",
	}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openClickHouse(t *testing.T) driver.Conn {
	t.Helper()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{e2eClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: e2eDatabase,
			Username: e2eCHUser,
			Password: e2eCHPassword,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := conn.Ping(context.Background()); err == nil {
			return conn
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("clickhouse not reachable")
	return nil
}

func waitForHTTP200(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", url)
}

func postCompletion(t *testing.T, gatewayURL, teamID string) {
	t.Helper()
	if err := postCompletionErr(gatewayURL, teamID); err != nil {
		t.Fatal(err)
	}
}

func postCompletionErr(gatewayURL, teamID string) error {
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/completions", strings.NewReader(
		`{"model":"mock-model","prompt":"hi","max_tokens":10}`,
	))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", teamID)

	resp, err := e2eHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("completion status=%d body=%s", resp.StatusCode, body)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func postEmbeddingErr(gatewayURL, teamID string) error {
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/embeddings", strings.NewReader(
		`{"model":"mock-model","input":"hello"}`,
	))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", teamID)

	resp, err := e2eHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("embedding status=%d body=%s", resp.StatusCode, body)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
