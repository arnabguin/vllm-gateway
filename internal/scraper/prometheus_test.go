package scraper

import "testing"

func TestParseGauge(t *testing.T) {
	text := []byte(`# HELP vllm:num_requests_running Number of running requests
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running 2
# HELP vllm:num_requests_waiting Number of waiting requests
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting 5
`)

	running, err := ParseGauge(text, "vllm:num_requests_running")
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if running != 2 {
		t.Fatalf("running=%v", running)
	}

	waiting, err := ParseGauge(text, "vllm:num_requests_waiting")
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if waiting != 5 {
		t.Fatalf("waiting=%v", waiting)
	}

	_, err = ParseGauge(text, "vllm:does_not_exist")
	if err == nil {
		t.Fatal("expected error for missing metric")
	}
}
