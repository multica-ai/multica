package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// iteratePages walks a paginated GitLab list endpoint, calling onPage with
// each batch of items as it arrives. It follows the "Link: ...; rel=\"next\""
// header until none is present (or onPage returns an error).
func iteratePages[T any](ctx context.Context, c *Client, token, path string, onPage func([]T) error) error {
	url := c.baseURL + "/api/v4" + path
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("gitlab: build request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("gitlab: http do: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return classifyHTTPError(resp.StatusCode, respBody)
		}

		var batch []T
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			resp.Body.Close()
			return fmt.Errorf("gitlab: decode page: %w", err)
		}
		linkHdr := resp.Header.Get("Link")
		resp.Body.Close()

		if err := onPage(batch); err != nil {
			return err
		}

		next := nextPageURL(linkHdr)
		if next == "" {
			return nil
		}
		url = next
	}
}

// classifyHTTPError mirrors the non-2xx classification in client.do so the
// pagination path produces the same sentinel errors callers expect.
func classifyHTTPError(status int, body []byte) error {
	var parsed struct {
		Message any `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	msg := formatGitlabMessage(parsed.Message)
	if msg == "" {
		msg = string(body)
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	default:
		return &APIError{StatusCode: status, Message: msg}
	}
}

// nextPageURL parses a `Link` header and returns the URL of the rel="next"
// entry, if any. Returns "" when the header is missing or has no next link.
//
// Format example:
//
//	<https://gitlab.com/api/v4/issues?page=2>; rel="next", <…?page=5>; rel="last"
func nextPageURL(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		open := strings.Index(part, "<")
		close := strings.Index(part, ">")
		if open == -1 || close == -1 || close <= open+1 {
			continue
		}
		return part[open+1 : close]
	}
	return ""
}
