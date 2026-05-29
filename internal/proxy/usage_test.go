package proxy

import "testing"

func TestParseCompletionResponse(t *testing.T) {
	body := []byte(`{"model":"m","usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	cr, err := ParseCompletionResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Model != "m" {
		t.Fatalf("model=%q", cr.Model)
	}
	if cr.Usage.PromptTokens != 10 {
		t.Fatalf("prompt=%d", cr.Usage.PromptTokens)
	}
	if cr.Usage.CompletionTokens != 5 {
		t.Fatalf("completion=%d", cr.Usage.CompletionTokens)
	}
}

func TestParseChatResponse(t *testing.T) {
	body := []byte(`{"model":"mock-model","usage":{"prompt_tokens":15,"completion_tokens":8}}`)
	cr, err := ParseChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Model != "mock-model" || cr.Usage.PromptTokens != 15 || cr.Usage.CompletionTokens != 8 {
		t.Fatalf("got model=%q prompt=%d completion=%d", cr.Model, cr.Usage.PromptTokens, cr.Usage.CompletionTokens)
	}
}

func TestParseUsageFromResponse(t *testing.T) {
	body := []byte(`{"model":"m","usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	paths := []string{"/v1/chat/completions", "/v1/completions"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			model, p, c, err := ParseUsageFromResponse(path, body)
			if err != nil || model != "m" || p != 10 || c != 5 {
				t.Fatalf("model=%q p=%d c=%d err=%v", model, p, c, err)
			}
		})
	}
}
