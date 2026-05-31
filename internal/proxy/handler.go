package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arnab-guin/vllm-gateway/internal/metrics"
	"github.com/arnab-guin/vllm-gateway/internal/storage"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics handler
type MetricsHandler struct {
}

func isMetricsPath(path string) bool {
	return path == "/v1/metrics"
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isMetricsPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	promhttp.Handler().ServeHTTP(w, r)
}

func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// Proxy Handler
type ProxyHandler struct {
	VLLMBaseURL string
	HTTPClient  *http.Client
	Storage     storage.Storage
	Metrics     *metrics.GatewayMetrics
}

func NewProxyHandler(vllmBaseURL string, client *http.Client, store storage.Storage, m *metrics.GatewayMetrics) *ProxyHandler {
	return &ProxyHandler{
		VLLMBaseURL: vllmBaseURL,
		HTTPClient:  client,
		Storage:     store,
		Metrics:     m,
	}
}

func isProxyPath(path string) bool {
	return path == "/v1/completions" ||
		path == "/v1/chat/completions" ||
		path == "/v1/embeddings"
}

type streamRequest struct {
	Stream bool `json:"stream"`
}

func parseStreamFlag(body []byte) (bool, error) {
	if len(body) == 0 {
		return false, nil
	}
	var req streamRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return false, err
	}
	return req.Stream, nil
}

func readRequestBodyAndCheckStream(r *http.Request) (body []byte, isStream bool, err error) {
	body, err = io.ReadAll(r.Body)
	if err != nil {
		return nil, false, err
	}
	isStream, err = parseStreamFlag(body)
	if err != nil {
		return nil, false, err
	}
	return body, isStream, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isProxyPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reqCtx, err := ExtractContext(r)
	if err != nil {
		if errors.Is(err, ErrMissingTeamID) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if h.Metrics != nil && h.Metrics.Counters != nil {
		if h.Metrics.Counters.Active != nil {
			h.Metrics.Counters.Active.Inc(reqCtx.TeamID)
			defer h.Metrics.Counters.Active.Dec(reqCtx.TeamID)
		}
		if h.Metrics.Counters.Total != nil {
			h.Metrics.Counters.Total.Inc(reqCtx.TeamID)
		}
	}

	base := strings.TrimRight(h.VLLMBaseURL, "/")
	upstreamURL := base + r.URL.Path

	requestBody, isStream, err := readRequestBodyAndCheckStream(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if isStream && r.URL.Path != "/v1/embeddings" {
		h.proxyStreaming(w, r, reqCtx, upstreamURL, requestBody)
		return
	}

	outReq, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		upstreamURL,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	for k, vals := range r.Header {
		for _, v := range vals {
			outReq.Header.Add(k, v)
		}
	}
	client := h.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	outResp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, "vllm unreachable", http.StatusServiceUnavailable)
		return
	}
	defer outResp.Body.Close()

	for k, vals := range outResp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	body, err := io.ReadAll(outResp.Body)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	var model string
	var promptTok, completionTok uint32

	if outResp.StatusCode >= 200 && outResp.StatusCode < 300 {
		var err error
		model, promptTok, completionTok, err = ParseUsageFromResponse(r.URL.Path, body)
		if err != nil {
			log.Printf("parse response path=%s: %v", r.URL.Path, err)
		} else {
			log.Printf("team=%s path=%s model=%s prompt_tokens=%d completion_tokens=%d",
				reqCtx.TeamID, r.URL.Path, model, promptTok, completionTok)
		}
	}

	latencyMS := uint32(time.Since(reqCtx.StartTime).Milliseconds())
	ttftMS := uint32(0) // populated when streaming TTFT is measured

	if h.Metrics != nil {
		if h.Metrics.Latency != nil {
			h.Metrics.Latency.Record(reqCtx.TeamID, latencyMS)
		}
		if h.Metrics.TTFT != nil && ttftMS > 0 {
			h.Metrics.TTFT.Record(reqCtx.TeamID, ttftMS)
		}
	}

	event := storage.RequestEvent{
		Timestamp:        time.Now(),
		TeamID:           reqCtx.TeamID,
		Project:          reqCtx.Project,
		UserID:           reqCtx.UserID,
		Model:            model,
		PromptTokens:     promptTok,
		CompletionTokens: completionTok,
		LatencyMS:        latencyMS,
		TTFTMS:           ttftMS,
		StatusCode:       uint16(outResp.StatusCode),
	}
	if err := h.Storage.InsertRequestEvent(r.Context(), event); err != nil {
		log.Printf("insert request event: %v", err)
	}
	w.WriteHeader(outResp.StatusCode)
	_, _ = w.Write(body)
}
