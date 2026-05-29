package storage

import (
	"time"
)

type RequestEvent struct {
	Timestamp        time.Time
	TeamID           string
	Project          string
	UserID           string
	Model            string
	PromptTokens     uint32
	CompletionTokens uint32
	LatencyMS        uint32
	TTFTMS           uint32
	StatusCode       uint16
	ErrorMessage     string
}
