package proxy

import (
	"encoding/json"
	"testing"
)

func TestInjectStreamIncludeUsage(t *testing.T) {
	in := []byte(`{"model":"m","stream":true,"messages":[]}`)
	out, err := injectStreamIncludeUsage(in)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	opts, ok := req["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options: %#v", req["stream_options"])
	}
}

func TestInjectStreamIncludeUsagePreservesOtherOptions(t *testing.T) {
	in := []byte(`{"stream":true,"stream_options":{"continuous_usage_stats":true}}`)
	out, err := injectStreamIncludeUsage(in)
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		StreamOptions struct {
			IncludeUsage           bool `json:"include_usage"`
			ContinuousUsageStats   bool `json:"continuous_usage_stats"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	if !req.StreamOptions.IncludeUsage || !req.StreamOptions.ContinuousUsageStats {
		t.Fatalf("got %+v", req.StreamOptions)
	}
}

func TestInjectStreamIncludeUsageNonStreamPassthrough(t *testing.T) {
	in := []byte(`{"stream":false,"model":"m"}`)
	out, err := injectStreamIncludeUsage(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Fatalf("got %s", out)
	}
}
