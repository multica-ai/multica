package daemon

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// personaHTTP is the small client used for the daemon's name-based sandbox
// assignment helpers. Kept short-timeout because we only do this on spawn,
// where slow responses turn into user-visible task delays.
var personaHTTP = &http.Client{Timeout: 3 * time.Second}

func newAuthedJSONRequest(ctx context.Context, method, url, token string, body []byte) (*http.Request, error) {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rdr == nil {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, rdr)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
