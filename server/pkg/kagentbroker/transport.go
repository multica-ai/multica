package kagentbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Endpoint is operator supplied. Client may install a qualified TLS transport;
// its redirect policy is always overridden. Plain HTTP is never accepted.
type Endpoint struct {
	URL        string
	Credential string
	Client     *http.Client
}
type endpoint struct {
	url        string
	credential string
	client     http.Client
}

func newEndpoint(c Endpoint) (endpoint, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !tokenSafe(c.Credential) {
		return endpoint{}, errDenied
	}
	origin := "https://" + u.Host
	if c.URL != origin && c.URL != origin+"/" {
		return endpoint{}, errDenied
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := http.Client{Timeout: 10 * time.Second, Transport: transport}
	if c.Client != nil {
		client = *c.Client
	}
	client.Jar = nil
	if client.Timeout <= 0 || client.Timeout > 10*time.Second {
		client.Timeout = 10 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return endpoint{strings.TrimSuffix(c.URL, "/"), c.Credential, client}, nil
}
func tokenSafe(s string) bool {
	if len(s) == 0 || len(s) > 16384 {
		return false
	}
	for _, c := range s {
		if c <= 32 || c >= 127 || c == ',' {
			return false
		}
	}
	return true
}

// upstreamResult carries the actual HTTP status, never an inferred Status body.
type upstreamResult struct {
	Status int
	Object map[string]any
}

func (e endpoint) call(ctx context.Context, method, path, ct string, body any) (map[string]any, error) {
	r, err := e.result(ctx, method, path, ct, body)
	if err != nil || r.Status < 200 || r.Status >= 300 {
		return nil, errDenied
	}
	return r.Object, nil
}
func (e endpoint) result(ctx context.Context, method, path, ct string, body any) (upstreamResult, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return upstreamResult{}, errDenied
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, e.url+path, bytes.NewReader(raw))
	if err != nil {
		return upstreamResult{}, errDenied
	}
	// No caller headers or caller-controlled authority cross this boundary.
	r.Header.Set("Authorization", "Bearer "+e.credential)
	r.Header.Set("Accept", "application/json")
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	resp, err := e.client.Do(r)
	if err != nil {
		return upstreamResult{}, errDenied
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamResult{Status: resp.StatusCode}, nil
	}
	raw, err = boundedRead(resp.Body, responseLimit)
	if err != nil {
		return upstreamResult{}, errDenied
	}
	m, err := parseObject(raw)
	return upstreamResult{Status: resp.StatusCode, Object: m}, err
}

// Identity is derived only from the Kagent cluster TokenReview API.
type Identity struct{ Username, UID string }
type Reviewer interface {
	Review(context.Context, string, string) (Identity, error)
}
type TokenReviewer struct{ endpoint endpoint }

func NewTokenReviewer(c Endpoint) (*TokenReviewer, error) {
	e, err := newEndpoint(c)
	if err != nil {
		return nil, err
	}
	return &TokenReviewer{e}, nil
}
func (t *TokenReviewer) Review(ctx context.Context, token, audience string) (Identity, error) {
	if t == nil || !tokenSafe(token) || audience == "" {
		return Identity{}, errDenied
	}
	m, err := t.endpoint.call(ctx, "POST", "/apis/authentication.k8s.io/v1/tokenreviews", "application/json", map[string]any{
		"apiVersion": "authentication.k8s.io/v1", "kind": "TokenReview", "spec": map[string]any{"token": token, "audiences": []string{audience}},
	})
	if err != nil || str(m["apiVersion"]) != "authentication.k8s.io/v1" || str(m["kind"]) != "TokenReview" {
		return Identity{}, errDenied
	}
	status := object(m["status"])
	if status["authenticated"] != true {
		return Identity{}, errDenied
	}
	if e, ok := status["error"]; ok && e != "" {
		return Identity{}, errDenied
	}
	audiences, ok := status["audiences"].([]any)
	if !ok || len(audiences) != 1 || audiences[0] != audience {
		return Identity{}, errDenied
	}
	user := object(status["user"])
	id := Identity{str(user["username"]), str(user["uid"])}
	if id.Username == "" || id.UID == "" {
		return Identity{}, errDenied
	}
	return id, nil
}
