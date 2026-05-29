package storage

import (
	"context"

	"github.com/arnab-guin/vllm-gateway/internal/scraper"
)

type Storage interface {
	InsertRequestEvent(ctx context.Context, e RequestEvent) error
	InsertRequestMetrics(ctx context.Context, m RequestMetrics) error
	InsertVLLMSystemMetrics(ctx context.Context, m scraper.VLLMSystemMetrics) error
	Ping(ctx context.Context) error
}
