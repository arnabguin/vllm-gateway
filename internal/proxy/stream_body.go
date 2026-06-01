package proxy

import "encoding/json"

// injectStreamIncludeUsage sets stream_options.include_usage on streaming requests so
// vLLM emits a final SSE chunk with token counts (OpenAI-compatible; off by default).
func injectStreamIncludeUsage(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return body, err
	}
	var stream bool
	if raw, ok := req["stream"]; ok {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return body, err
		}
	}
	if !stream {
		return body, nil
	}

	opts := map[string]bool{"include_usage": true}
	if raw, ok := req["stream_options"]; ok && len(raw) > 0 && string(raw) != "null" {
		var existing map[string]bool
		if err := json.Unmarshal(raw, &existing); err == nil {
			for k, v := range existing {
				opts[k] = v
			}
		}
	}
	opts["include_usage"] = true

	optsBytes, err := json.Marshal(opts)
	if err != nil {
		return body, err
	}
	req["stream_options"] = optsBytes

	return json.Marshal(req)
}
