package proxy

import (
	"net/http"

	"github.com/arnab-guin/vllm-gateway/internal/metrics"
)

// DebugEmitHandler triggers a single request_metrics flush (integration / debug only).
type DebugEmitHandler struct {
	Emitter *metrics.RequestMetricsEmitter
}

func (h *DebugEmitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Emitter == nil {
		http.Error(w, "emitter not configured", http.StatusServiceUnavailable)
		return
	}
	h.Emitter.EmitOnce(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
