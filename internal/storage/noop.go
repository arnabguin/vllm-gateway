package storage

import (
	"context"

	"github.com/arnab-guin/vllm-gateway/internal/scraper"
)

// NoopStorage drops events; use before ClickHouse is wired.
type NoopStorage struct{}

func NewNoopStorage() *NoopStorage {
	return &NoopStorage{}
}

func (s *NoopStorage) InsertRequestEvent(ctx context.Context, e RequestEvent) error {
	return nil
}

func (s *NoopStorage) InsertRequestMetrics(ctx context.Context, m RequestMetrics) error {
	return nil
}

func (s *NoopStorage) InsertVLLMSystemMetrics(ctx context.Context, m scraper.VLLMSystemMetrics) error {
	return nil
}

func (s *NoopStorage) Ping(ctx context.Context) error {
	return nil
}

var _ Storage = (*NoopStorage)(nil)
