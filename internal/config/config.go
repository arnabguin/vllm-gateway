package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type GatewayConfig struct {
	ListenAddr      string `yaml:"listen_addr"`
	EnableDebugEmit bool   `yaml:"enable_debug_emit"`
}

type VLLMConfig struct {
	URL                   string `yaml:"url"`
	EmbeddingsURL         string `yaml:"embeddings_url"`
	MetricsScrapeInterval string `yaml:"metrics_scrape_interval"`
}

type ClickHouseConfig struct {
	Addr     string `yaml:"addr"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type MetricsConfig struct {
	EmitInterval string `yaml:"emit_interval"`
}

type CostConfig struct {
	GPUUSDPerHour     float64 `yaml:"gpu_usd_per_hour"`
}

type Config struct {
	Gateway    GatewayConfig           `yaml:"gateway"`
	VLLM       VLLMConfig              `yaml:"vllm"`
	Clickhouse ClickHouseConfig        `yaml:"clickhouse"`
	Metrics    MetricsConfig           `yaml:"metrics"`
	Cost       CostConfig              `yaml:"cost"`
}

func Default() Config {
	return Config{
		Gateway: GatewayConfig{
			ListenAddr: ":8080",
		},
		VLLM: VLLMConfig{
			URL: "http://127.0.0.1:8000",
		},
		Clickhouse: ClickHouseConfig{
			Database: "vllm_attribution",
		},
	}
}

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("GATEWAY_LISTEN_ADDR"); v != "" {
		cfg.Gateway.ListenAddr = v
	}
	if v := os.Getenv("VLLM_URL"); v != "" {
		cfg.VLLM.URL = v
	}
	if v, ok := os.LookupEnv("VLLM_EMBEDDINGS_URL"); ok {
		cfg.VLLM.EmbeddingsURL = v
	}
	if v := os.Getenv("VLLM_METRICS_SCRAPE_INTERVAL"); v != "" {
		cfg.VLLM.MetricsScrapeInterval = v
	}
	if v := os.Getenv("CLICKHOUSE_ADDR"); v != "" {
		cfg.Clickhouse.Addr = v
	}
	if v := os.Getenv("CLICKHOUSE_DATABASE"); v != "" {
		cfg.Clickhouse.Database = v
	}
	if v := os.Getenv("CLICKHOUSE_USERNAME"); v != "" {
		cfg.Clickhouse.Username = v
	}
	if v := os.Getenv("CLICKHOUSE_PASSWORD"); v != "" {
		cfg.Clickhouse.Password = v
	}
	if v := os.Getenv("METRICS_EMIT_INTERVAL"); v != "" {
		cfg.Metrics.EmitInterval = v
	}
	if v := os.Getenv("GPU_USD_PER_HOUR"); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("GPU_USD_PER_HOUR: %w", err)
		}
		cfg.Cost.GPUUSDPerHour = rate
	}
	return nil
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s : %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, fmt.Errorf("apply env overrides: %w", err)
	}
	
	return &cfg, nil
}
