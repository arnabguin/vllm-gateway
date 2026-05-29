package proxy

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/arnab-guin/vllm-gateway/internal/storage"
)

// proxyStreaming relays upstream SSE, records TTFT on the first content token,
// and persists request_events + Prometheus histograms when the stream completes.
func (h *ProxyHandler) proxyStreaming(
	w http.ResponseWriter,
	r *http.Request,
	reqCtx RequestContext,
	upstreamURL string,
	requestBody []byte,
) {
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
		client = &http.Client{Timeout: 0}
	}

	outResp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, "vllm unreachable", http.StatusServiceUnavailable)
		return
	}
	defer outResp.Body.Close()

	copyHeaders(w.Header(), outResp.Header)
	w.WriteHeader(outResp.StatusCode)

	// If upstream returned an error response, passthrough the body and record an event.
	if outResp.StatusCode < http.StatusOK || outResp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(outResp.Body)
		if readErr == nil {
			_, _ = w.Write(body)
		}
		h.recordRequestMetricsAndEvent(r.Context(), reqCtx, "", 0, 0, 0, outResp.StatusCode)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	parser := NewSSEParser()
	buf := make([]byte, 4096)

	var ttftMS uint32
	ttftRecorded := false
	var model string
	var promptTok, completionTok uint32
	streamDone := false

	for {
		n, readErr := outResp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, err := w.Write(chunk); err != nil {
				break
			}
			flusher.Flush()

			payloads := parser.Push(chunk)
			for _, payload := range payloads {
				payloadBytes := []byte(payload)
				if isStreamDonePayload(payloadBytes) {
					streamDone = true
				}
				if !ttftRecorded && hasFirstTokenContent(r.URL.Path, payloadBytes) {
					ttftMS = uint32(time.Since(reqCtx.StartTime).Milliseconds())
					ttftRecorded = true
				}
				if m, pt, ct, ok := extractStreamModelAndUsage(payloadBytes); ok {
					if m != "" {
						model = m
					}
					if pt > 0 || ct > 0 {
						promptTok = pt
						completionTok = ct
					}
				}
			}
		}

		if streamDone || readErr == io.EOF {
			break
		}
		if readErr != nil {
			log.Printf("stream read error path=%s: %v", r.URL.Path, readErr)
			break
		}
	}

	h.recordRequestMetricsAndEvent(
		r.Context(),
		reqCtx,
		model,
		promptTok,
		completionTok,
		ttftMS,
		outResp.StatusCode,
	)
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func (h *ProxyHandler) recordRequestMetricsAndEvent(
	ctx context.Context,
	reqCtx RequestContext,
	model string,
	promptTok, completionTok, ttftMS uint32,
	statusCode int,
) {
	latencyMS := uint32(time.Since(reqCtx.StartTime).Milliseconds())

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
		StatusCode:       uint16(statusCode),
	}
	if err := h.Storage.InsertRequestEvent(ctx, event); err != nil {
		log.Printf("insert request event: %v", err)
	}
}
