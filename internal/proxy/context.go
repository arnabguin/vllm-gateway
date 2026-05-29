package proxy

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

var ErrMissingTeamID = errors.New("missing X-Team-ID header")

type RequestContext struct {
	TeamID    string
	Project   string
	UserID    string
	StartTime time.Time
}

func ExtractContext(r *http.Request) (RequestContext, error) {
	teamID := strings.TrimSpace(r.Header.Get("X-Team-ID"))
	if teamID == "" {
		return RequestContext{}, ErrMissingTeamID
	}
	project := strings.TrimSpace(r.Header.Get("X-Project"))
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))

	return RequestContext{
		TeamID:    teamID,
		Project:   project,
		UserID:    userID,
		StartTime: time.Now(),
	}, nil
}
