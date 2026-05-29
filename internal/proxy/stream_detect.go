package proxy

import (
	"encoding/json"
	"strings"
)

func isStreamDonePayload(payload []byte) bool {
	return strings.EqualFold(strings.TrimSpace(string(payload)), "[DONE]")
}

// hasFirstTokenContent reports whether one SSE data payload carries
// the first content token for TTFT measurement.
func hasFirstTokenContent(path string, payload []byte) bool {
	if isStreamDonePayload(payload) {
		return false
	}
	event, err := parseStreamPayload(payload)
	if err != nil {
		return false
	}
	for _, c := range event.Choices {
		switch path {
		case "/v1/chat/completions":
			if c.Delta.Content != "" {
				return true
			}
		case "/v1/completions":
			if c.Text != "" {
				return true
			}
		}
	}
	return false
}

// minimal skeleton for stream payload decoding.
type streamChoiceDelta struct {
	Content string `json:"content"`
}

type streamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type streamChoice struct {
	Text  string            `json:"text"`
	Delta streamChoiceDelta `json:"delta"`
}

type streamPayload struct {
	Model   string         `json:"model"`
	Usage   streamUsage    `json:"usage"`
	Choices []streamChoice `json:"choices"`
}

func parseStreamPayload(payload []byte) (streamPayload, error) {
	var out streamPayload
	if err := json.Unmarshal(payload, &out); err != nil {
		return streamPayload{}, err
	}
	return out, nil
}

func extractStreamModelAndUsage(payload []byte) (model string, promptTok, completionTok uint32, ok bool) {
	if isStreamDonePayload(payload) {
		return "", 0, 0, false
	}
	event, err := parseStreamPayload(payload)
	if err != nil {
		return "", 0, 0, false
	}
	if event.Model == "" && event.Usage.PromptTokens == 0 && event.Usage.CompletionTokens == 0 {
		return "", 0, 0, false
	}
	return event.Model, uint32(event.Usage.PromptTokens), uint32(event.Usage.CompletionTokens), true
}
