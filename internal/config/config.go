package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type GatewayConfig struct {
	ListenAddr      string `yaml:"listen_addr"`
	EnableDebugEmit bool   `yaml:"enable_debug_emit"`
}

type VLLMConfig struct {
	URL                   string `yaml:"url"`
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

type ModelPricing struct {
	Prompt     float64 `yaml:"prompt"`
	Completion float64 `yaml:"completion"`
}

type Config struct {
	Gateway    GatewayConfig           `yaml:"gateway"`
	VLLM       VLLMConfig              `yaml:"vllm"`
	Clickhouse ClickHouseConfig        `yaml:"clickhouse"`
	Metrics    MetricsConfig           `yaml:"metrics"`
	Pricing    map[string]ModelPricing `yaml:"pricing"`
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

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s : %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil

}
