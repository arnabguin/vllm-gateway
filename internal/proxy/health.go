package proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type HealthHandler struct {
	VLLMBaseURL string
	HTTPClient  *http.Client
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if vllmReachable := h.vllmReachable(ctx); !vllmReachable {
		http.Error(w, "vllm unreachable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte("ok"))
	}
}

func (h *HealthHandler) vllmReachable(ctx context.Context) bool {
	base := strings.TrimRight(h.VLLMBaseURL, "/")
	url := base + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("Unable to create vllm reachability request (%s)", url)
		return false
	}
	client := h.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}
	return false
}
