package proxy

import (
	"encoding/json"
	"fmt"
)

type ApiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type CompletionResponse struct {
	Model string   `json:"model"`
	Usage ApiUsage `json:"usage"`
}

type ChatResponse struct {
	Model string   `json:"model"`
	Usage ApiUsage `json:"usage"`
}

func ParseCompletionResponse(body []byte) (CompletionResponse, error) {
	cr := CompletionResponse{}

	if err := json.Unmarshal(body, &cr); err != nil {
		return cr, err
	}
	return cr, nil
}

func ParseChatResponse(body []byte) (ChatResponse, error) {
	cr := ChatResponse{}

	if err := json.Unmarshal(body, &cr); err != nil {
		return cr, err
	}
	return cr, nil
}

func ParseUsageFromResponse(path string, body []byte) (model string, promptTok, completionTok uint32, err error) {
	switch path {
	case "/v1/chat/completions":
		cr, err := ParseChatResponse(body)
		if err != nil {
			return "", 0, 0, fmt.Errorf("parse chat: %w", err)
		}
		return cr.Model, uint32(cr.Usage.PromptTokens), uint32(cr.Usage.CompletionTokens), nil
	case "/v1/completions":
		cr, err := ParseCompletionResponse(body)
		if err != nil {
			return "", 0, 0, fmt.Errorf("parse completion: %w", err)
		}
		return cr.Model, uint32(cr.Usage.PromptTokens), uint32(cr.Usage.CompletionTokens), nil
	default:
		return "", 0, 0, fmt.Errorf("unknown path %s", path)
	}
}
