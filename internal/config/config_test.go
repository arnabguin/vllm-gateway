package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`gateway:
  listen_addr: ":8080"
vllm:
  url: "http://127.0.0.1:8000"
  embeddings_url: "http://127.0.0.1:8001"
clickhouse:
  addr: "127.0.0.1:9000"
  database: "vllm_attribution"
  username: "default"
  password: "devpassword"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VLLM_URL", "http://mock-vllm:8000")
	t.Setenv("VLLM_EMBEDDINGS_URL", "http://host.docker.internal:8001")
	t.Setenv("CLICKHOUSE_ADDR", "clickhouse:9000")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VLLM.URL != "http://mock-vllm:8000" {
		t.Fatalf("VLLM.URL = %q, want mock-vllm", cfg.VLLM.URL)
	}
	if cfg.VLLM.EmbeddingsURL != "http://host.docker.internal:8001" {
		t.Fatalf("EmbeddingsURL = %q", cfg.VLLM.EmbeddingsURL)
	}
	if cfg.Clickhouse.Addr != "clickhouse:9000" {
		t.Fatalf("Clickhouse.Addr = %q", cfg.Clickhouse.Addr)
	}
}

func TestLoadClearsEmbeddingsURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`vllm:
  url: "http://127.0.0.1:8000"
  embeddings_url: "http://127.0.0.1:8001"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VLLM_EMBEDDINGS_URL", "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VLLM.EmbeddingsURL != "" {
		t.Fatalf("EmbeddingsURL = %q, want empty", cfg.VLLM.EmbeddingsURL)
	}
}
