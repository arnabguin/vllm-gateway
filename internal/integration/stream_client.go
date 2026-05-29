package integration

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	e2eGatewayURL     = "http://127.0.0.1:18080"
	e2eClickHouseAddr = "127.0.0.1:19000"
	e2eDatabase       = "vllm_attribution"
	e2eCHUser         = "default"
	e2eCHPassword     = "devpassword"
)

var e2eHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 32,
	},
}

// readSSEDataPayloads extracts data: lines from a full SSE response body.
func readSSEDataPayloads(body []byte) []string {
	var payloads []string
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := bytes.TrimSpace(line[len("data:"):])
		if len(value) == 0 {
			continue
		}
		payloads = append(payloads, string(value))
	}
	return payloads
}

func validateChatStreamPayloads(payloads []string) error {
	if len(payloads) < 4 {
		return fmt.Errorf("chat stream payloads: got %d want >= 4", len(payloads))
	}
	if payloads[len(payloads)-1] != "[DONE]" {
		return fmt.Errorf("chat stream last payload: got %q want [DONE]", payloads[len(payloads)-1])
	}
	var sawRole, sawHi, sawThere, sawUsage bool
	for _, p := range payloads {
		switch {
		case strings.Contains(p, `"role": "assistant"`):
			sawRole = true
		case strings.Contains(p, `"content": "Hi"`):
			sawHi = true
		case strings.Contains(p, `"content": " there"`):
			sawThere = true
		case strings.Contains(p, `"prompt_tokens": 15`) && strings.Contains(p, `"completion_tokens": 8`):
			sawUsage = true
		}
	}
	if !sawRole {
		return fmt.Errorf("chat stream: missing role-only chunk")
	}
	if !sawHi || !sawThere {
		return fmt.Errorf("chat stream: missing content chunks (hi=%v there=%v)", sawHi, sawThere)
	}
	if !sawUsage {
		return fmt.Errorf("chat stream: missing usage chunk")
	}
	return nil
}

func validateCompletionsStreamPayloads(payloads []string) error {
	if len(payloads) < 4 {
		return fmt.Errorf("completions stream payloads: got %d want >= 4", len(payloads))
	}
	if payloads[len(payloads)-1] != "[DONE]" {
		return fmt.Errorf("completions stream last payload: got %q want [DONE]", payloads[len(payloads)-1])
	}
	var sawHel, sawLo, sawUsage bool
	for _, p := range payloads {
		switch {
		case strings.Contains(p, `"text": "Hel"`):
			sawHel = true
		case strings.Contains(p, `"text": "lo"`):
			sawLo = true
		case strings.Contains(p, `"prompt_tokens": 12`) && strings.Contains(p, `"completion_tokens": 7`):
			sawUsage = true
		}
	}
	if !sawHel || !sawLo {
		return fmt.Errorf("completions stream: missing text chunks (hel=%v lo=%v)", sawHel, sawLo)
	}
	if !sawUsage {
		return fmt.Errorf("completions stream: missing usage chunk")
	}
	return nil
}

func assertChatStreamPayloads(t *testing.T, payloads []string) {
	t.Helper()
	if err := validateChatStreamPayloads(payloads); err != nil {
		t.Fatal(err)
	}
}

func assertCompletionsStreamPayloads(t *testing.T, payloads []string) {
	t.Helper()
	if err := validateCompletionsStreamPayloads(payloads); err != nil {
		t.Fatal(err)
	}
}

func postStreamChatCompletionErr(gatewayURL, teamID string) ([]string, error) {
	return postStreamErr(gatewayURL, teamID, "/v1/chat/completions",
		`{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10,"stream":true}`)
}

func postStreamCompletionErr(gatewayURL, teamID string) ([]string, error) {
	return postStreamErr(gatewayURL, teamID, "/v1/completions",
		`{"model":"mock-model","prompt":"hi","max_tokens":10,"stream":true}`)
}

func postStreamErr(gatewayURL, teamID, path, jsonBody string) ([]string, error) {
	req, err := http.NewRequest(http.MethodPost, gatewayURL+path, strings.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Team-ID", teamID)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := e2eHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stream %s status=%d body=%s", path, resp.StatusCode, body)
	}
	return readSSEDataPayloads(body), nil
}

func postStreamsChatConcurrent(t *testing.T, gatewayURL string, teams []string, requestsPerTeam int) {
	t.Helper()

	var wg sync.WaitGroup
	errCh := make(chan error, len(teams)*requestsPerTeam)

	for _, teamID := range teams {
		for range requestsPerTeam {
			wg.Add(1)
			go func(team string) {
				defer wg.Done()
				payloads, err := postStreamChatCompletionErr(gatewayURL, team)
				if err != nil {
					errCh <- fmt.Errorf("team %q: %w", team, err)
					return
				}
				if err := validateChatStreamPayloads(payloads); err != nil {
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
