package vibeshandoff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrRejected = errors.New("VIBES handoff rejected")

type Identity struct {
	UserID        string `json:"userId"`
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceSlug string `json:"workspaceSlug"`
	WorkspaceName string `json:"workspaceName"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
}

type Client struct {
	consumeURL string
	secret     string
	httpClient *http.Client
}

func NewClient(consumeURL, secret string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(consumeURL))
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" {
		return nil, errors.New("VIBES handoff consume URL must be local HTTP")
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("VIBES handoff consume URL must be loopback-only")
	}
	if len(secret) < 32 {
		return nil, errors.New("VIBES handoff service secret is not configured")
	}
	return &Client{
		consumeURL: parsed.String(),
		secret:     secret,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) Consume(ctx context.Context, code, audience string) (Identity, error) {
	body, err := json.Marshal(map[string]string{
		"code":     code,
		"audience": audience,
	})
	if err != nil {
		return Identity{}, ErrRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.consumeURL, bytes.NewReader(body))
	if err != nil {
		return Identity{}, ErrRejected
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Identity{}, ErrRejected
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Identity{}, ErrRejected
	}
	var identity Identity
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return Identity{}, ErrRejected
	}
	if identity.UserID == "" || identity.WorkspaceID == "" || identity.WorkspaceSlug == "" || identity.WorkspaceName == "" || identity.Name == "" || identity.Email == "" {
		return Identity{}, ErrRejected
	}
	if identity.Role != "owner" && identity.Role != "member" {
		return Identity{}, fmt.Errorf("%w", ErrRejected)
	}
	return identity, nil
}
