package proxy

import "testing"

func TestHasFirstTokenContent_DONE(t *testing.T) {
	if hasFirstTokenContent("/v1/chat/completions", []byte("[DONE]")) {
		t.Fatal("expected false for [DONE]")
	}
}

func TestHasFirstTokenContent_ChatRoleOnly(t *testing.T) {
	payload := []byte(`{"choices":[{"delta":{"role":"assistant"},"index":0}]}`)
	if hasFirstTokenContent("/v1/chat/completions", payload) {
		t.Fatal("role-only delta should not count as first token")
	}
}

func TestHasFirstTokenContent_ChatEmptyContent(t *testing.T) {
	payload := []byte(`{"choices":[{"delta":{"content":""},"index":0}]}`)
	if hasFirstTokenContent("/v1/chat/completions", payload) {
		t.Fatal("empty content should not count as first token")
	}
}

func TestHasFirstTokenContent_ChatFirstContent(t *testing.T) {
	payload := []byte(`{"choices":[{"delta":{"content":"Hel"},"index":0}]}`)
	if !hasFirstTokenContent("/v1/chat/completions", payload) {
		t.Fatal("expected true for first non-empty delta.content")
	}
}

func TestHasFirstTokenContent_CompletionsEmptyText(t *testing.T) {
	payload := []byte(`{"choices":[{"text":"","index":0}]}`)
	if hasFirstTokenContent("/v1/completions", payload) {
		t.Fatal("empty text should not count as first token")
	}
}

func TestHasFirstTokenContent_CompletionsFirstText(t *testing.T) {
	payload := []byte(`{"choices":[{"text":"Hel","index":0}]}`)
	if !hasFirstTokenContent("/v1/completions", payload) {
		t.Fatal("expected true for first non-empty text")
	}
}

func TestHasFirstTokenContent_InvalidJSON(t *testing.T) {
	if hasFirstTokenContent("/v1/chat/completions", []byte(`not json`)) {
		t.Fatal("invalid JSON should return false")
	}
}

func TestHasFirstTokenContent_UnknownPath(t *testing.T) {
	payload := []byte(`{"choices":[{"delta":{"content":"x"}}]}`)
	if hasFirstTokenContent("/v1/unknown", payload) {
		t.Fatal("unknown path should return false")
	}
}

func TestExtractStreamModelAndUsage_DONE(t *testing.T) {
	_, _, _, ok := extractStreamModelAndUsage([]byte("[DONE]"))
	if ok {
		t.Fatal("expected false for [DONE]")
	}
}

func TestExtractStreamModelAndUsage_NoUsage(t *testing.T) {
	payload := []byte(`{"choices":[{"delta":{"content":"hi"}}]}`)
	_, _, _, ok := extractStreamModelAndUsage(payload)
	if ok {
		t.Fatal("expected false when model and usage absent")
	}
}

func TestExtractStreamModelAndUsage_WithUsage(t *testing.T) {
	payload := []byte(`{"model":"mock-model","usage":{"prompt_tokens":12,"completion_tokens":7}}`)
	model, pt, ct, ok := extractStreamModelAndUsage(payload)
	if !ok {
		t.Fatal("expected ok")
	}
	if model != "mock-model" || pt != 12 || ct != 7 {
		t.Fatalf("got model=%q pt=%d ct=%d", model, pt, ct)
	}
}
