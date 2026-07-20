package cli

// FIR-3548 — the internal browser verifier answers a failed verification with a
// structured 422 diagnostic body (attempted address, stage, cause, screenshot).
// PostJSON truncates an error body at 4 KiB, which is far below a PNG, so this
// endpoint gets its own request path that decodes the failure body in full.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// diagnosticBodyLimit matches the verifier's own screenshot ceiling plus room
// for the surrounding JSON.
const diagnosticBodyLimit = (10 << 20) + (1 << 20)

// PostJSONWithDiagnostic posts body to path and decodes the response into out
// for both a success and an expected failure status. It returns the status code
// so the caller can tell the two apart; out is populated in both cases.
// Any other status is returned as a normal HTTPError.
func (c *APIClient) PostJSONWithDiagnostic(ctx context.Context, path string, body any, out any, diagnosticStatus int) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != diagnosticStatus {
		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, &HTTPError{
			Method:     http.MethodPost,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       string(bytes.TrimSpace(respData)),
		}
	}
	if out == nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, diagnosticBodyLimit)).Decode(out)
}
