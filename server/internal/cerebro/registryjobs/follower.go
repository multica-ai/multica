// Package registryjobs follows the Data Registry's marker-gated adaptive
// execute responses without exposing connection credentials to callers.
package registryjobs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	MarkerHeader = "X-Registry-Async"
	MarkerValue  = "job-v1"
	maxPolls     = 60
	maxBackoff   = 2 * time.Second
)

// Enable opts a Registry v1 execute request into adaptive 200/202 handling.
func Enable(request *http.Request) {
	if request != nil {
		request.Header.Set(MarkerHeader, MarkerValue)
	}
}

// IsV1BaseURL reports whether a configured connection base points at the
// Registry v1 surface. Generic API connections only opt into job semantics for
// this base, so unrelated APIs never receive Registry-specific headers.
func IsV1BaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	return err == nil && strings.HasSuffix(parsed.Path, "/api/registry/v1")
}

// Follow polls a marked 202 response until Registry returns a terminal HTTP
// response. Polls preserve the original request headers (auth, delegation and
// BigQuery labels), but may only target a relative same-origin Location under
// allowedBaseURL's path.
func Follow(
	ctx context.Context,
	client *http.Client,
	original *http.Request,
	response *http.Response,
	allowedBaseURL string,
) (*http.Response, error) {
	if !isPending(response) {
		return response, nil
	}
	if client == nil || original == nil || original.URL == nil {
		return response, fmt.Errorf("registry job follower is not configured")
	}
	allowed, err := url.Parse(strings.TrimRight(allowedBaseURL, "/"))
	if err != nil || allowed.Scheme == "" || allowed.Host == "" {
		return response, fmt.Errorf("registry job follower has invalid base URL")
	}

	for poll := 0; poll < maxPolls && isPending(response); poll++ {
		location := response.Header.Get("Location")
		pollURL, err := safePollURL(location, original.URL, allowed)
		if err != nil {
			response.Body.Close()
			return nil, err
		}
		delay := pollDelay(response.Header.Get("Retry-After"), poll)
		response.Body.Close()
		if err := wait(ctx, delay); err != nil {
			return nil, fmt.Errorf("registry job polling stopped: %w", err)
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("build registry job poll: %w", err)
		}
		request.Header = original.Header.Clone()
		request.Header.Del("Content-Length")
		request.Header.Del("Content-Type")
		response, err = client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("poll registry job: %w", err)
		}
	}
	if isPending(response) {
		response.Body.Close()
		return nil, fmt.Errorf("registry job exceeded %d polls", maxPolls)
	}
	return response, nil
}

func isPending(response *http.Response) bool {
	return response != nil &&
		response.StatusCode == http.StatusAccepted &&
		response.Header.Get(MarkerHeader) == MarkerValue
}

func safePollURL(location string, original, allowed *url.URL) (*url.URL, error) {
	if strings.TrimSpace(location) == "" {
		return nil, fmt.Errorf("registry job response has no Location")
	}
	reference, err := url.Parse(location)
	if err != nil || reference.IsAbs() || reference.Host != "" {
		return nil, fmt.Errorf("registry job Location must be relative and same-origin")
	}
	resolved := original.ResolveReference(reference)
	if !sameOrigin(resolved, allowed) || !withinBasePath(resolved.Path, allowed.Path) {
		return nil, fmt.Errorf("registry job Location is outside the approved connection base path")
	}
	return resolved, nil
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func withinBasePath(path, base string) bool {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "/"
	}
	return path == base || base == "/" || strings.HasPrefix(path, base+"/")
}

func pollDelay(retryAfter string, poll int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	delay := 100 * time.Millisecond * time.Duration(1<<min(poll, 4))
	if delay > maxBackoff {
		return maxBackoff
	}
	return delay
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
