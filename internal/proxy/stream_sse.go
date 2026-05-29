package proxy

import (
	"bytes"
	"strings"
)

// SSEParser incrementally parses Server-Sent Events and emits complete data payloads.
type SSEParser struct {
	buffer    []byte
	dataLines []string
}

func NewSSEParser() *SSEParser {
	return &SSEParser{}
}

// Push appends bytes and returns complete SSE event payloads.
// Each returned payload corresponds to one event's data lines joined with '\n'.
func (p *SSEParser) Push(chunk []byte) []string {
	p.buffer = append(p.buffer, chunk...)

	var out []string
	for {
		line, ok := p.nextLine()
		if !ok {
			return out
		}
		trimmed := strings.TrimSuffix(line, "\r")
		if trimmed == "" {
			if payload, ok := p.flushEvent(); ok {
				out = append(out, payload)
			}
			continue
		}
		if data, ok := parseSSEDataLine(trimmed); ok {
			p.dataLines = append(p.dataLines, data)
		}
	}
}

func (p *SSEParser) nextLine() (string, bool) {
	idx := bytes.IndexByte(p.buffer, '\n')
	if idx < 0 {
		return "", false
	}
	line := string(p.buffer[:idx])
	p.buffer = p.buffer[idx+1:]
	return line, true
}

func (p *SSEParser) flushEvent() (string, bool) {
	if len(p.dataLines) == 0 {
		return "", false
	}
	payload := strings.Join(p.dataLines, "\n")
	p.dataLines = p.dataLines[:0]
	return payload, true
}

// parseSSEDataLine extracts the data content from a single SSE line.
// It returns (value, true) when line starts with "data:".
func parseSSEDataLine(line string) (string, bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimLeft(line[len("data:"):], " "), true
}
