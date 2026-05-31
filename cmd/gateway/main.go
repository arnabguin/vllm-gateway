package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/arnab-guin/vllm-gateway/internal/config"
	"github.com/arnab-guin/vllm-gateway/internal/metrics"
	"github.com/arnab-guin/vllm-gateway/internal/proxy"
	"github.com/arnab-guin/vllm-gateway/internal/scraper"
	"github.com/arnab-guin/vllm-gateway/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	log.Printf("config loaded: listen=%s vllm=%s db=%s",
		cfg.Gateway.ListenAddr,
		cfg.VLLM.URL,
		cfg.Clickhouse.Database,
	)

	var store storage.Storage
	ch, err := storage.NewClickHouseStorage(cfg.Clickhouse)
	if err != nil {
		log.Printf("clickhouse unavailable, using noop storage: %v", err)
		store = storage.NewNoopStorage()
	} else {
		store = ch
		log.Printf("clickhouse connected: addr=%s database=%s", cfg.Clickhouse.Addr, cfg.Clickhouse.Database)
	}

	httpClient := &http.Client{
		Timeout: 120 * time.Second,
	}

	scrapeInterval := 15 * time.Second
	if cfg.VLLM.MetricsScrapeInterval != "" {
		if d, err := time.ParseDuration(cfg.VLLM.MetricsScrapeInterval); err == nil {
			scrapeInterval = d
		} else {
			log.Printf("invalid vllm.metrics_scrape_interval %q, using 15s: %v", cfg.VLLM.MetricsScrapeInterval, err)
		}
	}
	vllmScraper := scraper.NewVLLMScraper(cfg.VLLM.URL, scrapeInterval, httpClient)
	vllmScraper.Start(context.Background(), store)
	log.Printf("vllm metrics scraper started: interval=%s", scrapeInterval)

	emitInterval := 15 * time.Second
	if cfg.Metrics.EmitInterval != "" {
		if d, err := time.ParseDuration(cfg.Metrics.EmitInterval); err == nil {
			emitInterval = d
		} else {
			log.Printf("invalid metrics.emit_interval %q, using 15s: %v", cfg.Metrics.EmitInterval, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gwMetrics := metrics.NewGatewayMetrics()
	gwMetrics.Start(ctx)

	metricsEmitter := metrics.NewRequestMetricsEmitter(store, emitInterval, gwMetrics)
	if emitInterval > 0 {
		metricsEmitter.Start(ctx)
		log.Printf("request_metrics emit started: interval=%s", emitInterval)
	} else {
		log.Printf("request_metrics periodic emit disabled (emit_interval=0)")
	}

	health := &proxy.HealthHandler{
		VLLMBaseURL: cfg.VLLM.URL,
		HTTPClient:  httpClient,
	}
	completions := proxy.NewProxyHandler(cfg.VLLM.URL, httpClient, store, gwMetrics)
	metricsHandler := proxy.NewMetricsHandler()

	mux := http.NewServeMux()
	mux.Handle("/health", health)
	mux.Handle("/v1/completions", completions)
	mux.Handle("/v1/chat/completions", completions)
	mux.Handle("/v1/embeddings", completions)
	mux.Handle("/v1/metrics", metricsHandler)
	if cfg.Gateway.EnableDebugEmit {
		mux.Handle("/debug/emit-request-metrics", &proxy.DebugEmitHandler{Emitter: metricsEmitter})
		log.Printf("debug emit endpoint enabled: POST /debug/emit-request-metrics")
	}

	srv := &http.Server{
		Addr:    cfg.Gateway.ListenAddr,
		Handler: mux,
	}

	log.Printf("listening on %s", cfg.Gateway.ListenAddr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
