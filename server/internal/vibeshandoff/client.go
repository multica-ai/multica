package vibeshandoff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrRejected = errors.New("VIBES handoff rejected")

const CLIAudience = "vibes-multica-cli-v1"
const CLISchemaVersion = 1

type Identity struct {
	UserID                     string `json:"userId"`
	SessionID                  string `json:"sessionId"`
	WorkspaceID                string `json:"workspaceId"`
	WorkspaceSlug              string `json:"workspaceSlug"`
	WorkspaceName              string `json:"workspaceName"`
	Name                       string `json:"name"`
	Email                      string `json:"email"`
	Role                       string `json:"role"`
	AccountEpoch               uint64 `json:"accountEpoch"`
	SessionWorkspaceGeneration uint64 `json:"sessionWorkspaceGeneration"`
	AuthorityVersion           uint64 `json:"authorityVersion"`
	MembershipGeneration       uint64 `json:"membershipGeneration"`
}

type Client struct {
	consumeURL string
	secret     string
	httpClient *http.Client
}

type CLIConsumeRequest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Code          string `json:"code"`
	CodeVerifier  string `json:"codeVerifier"`
	ReceiverID    string `json:"receiverId"`
	ReceiverURI   string `json:"receiverUri"`
	Audience      string `json:"audience"`
}

type CLIIdentity struct {
	Identity
	SchemaVersion    int       `json:"schemaVersion"`
	SessionExpiresAt time.Time `json:"sessionExpiresAt"`
}

func NewCLIClient(consumeURL, secret string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(consumeURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "/api/tag-cli/consume" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("VIBES CLI consume URL is invalid")
	}
	ip := net.ParseIP(parsed.Hostname())
	loopback := parsed.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return nil, errors.New("VIBES CLI consume URL must use HTTPS")
	}
	if len(secret) < 32 {
		return nil, errors.New("VIBES CLI exchange service secret is not configured")
	}
	return &Client{
		consumeURL: parsed.String(),
		secret:     secret,
		httpClient: &http.Client{
			Timeout:       3 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// ValidateCLIConfig allows an explicitly disabled all-empty configuration, but
// rejects partial or invalid service wiring before an authorization code can
// be forwarded.
func ValidateCLIConfig(consumeURL, secret string) error {
	if strings.TrimSpace(consumeURL) == "" && strings.TrimSpace(secret) == "" {
		return nil
	}
	_, err := NewCLIClient(consumeURL, secret)
	return err
}

func (c *Client) ConsumeCLI(ctx context.Context, input CLIConsumeRequest) (CLIIdentity, error) {
	if input.SchemaVersion != CLISchemaVersion || input.Audience != CLIAudience || input.Code == "" || input.CodeVerifier == "" || input.ReceiverID == "" || input.ReceiverURI == "" {
		return CLIIdentity{}, ErrRejected
	}
	body, err := json.Marshal(input)
	if err != nil {
		return CLIIdentity{}, ErrRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.consumeURL, bytes.NewReader(body))
	if err != nil {
		return CLIIdentity{}, ErrRejected
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return CLIIdentity{}, ErrRejected
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return CLIIdentity{}, ErrRejected
	}
	var identity CLIIdentity
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil || identity.SchemaVersion != CLISchemaVersion || identity.SessionExpiresAt.IsZero() || !validIdentity(identity.Identity) {
		return CLIIdentity{}, ErrRejected
	}
	return identity, nil
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
	if !validIdentity(identity) {
		return Identity{}, ErrRejected
	}
	return identity, nil
}

func validIdentity(identity Identity) bool {
	if identity.UserID == "" || identity.SessionID == "" || identity.WorkspaceID == "" || identity.WorkspaceSlug == "" || identity.WorkspaceName == "" || identity.Name == "" ||
		identity.AccountEpoch == 0 || identity.AccountEpoch > math.MaxInt64 ||
		identity.SessionWorkspaceGeneration == 0 || identity.SessionWorkspaceGeneration > math.MaxInt64 ||
		identity.AuthorityVersion == 0 || identity.AuthorityVersion > math.MaxInt64 ||
		identity.MembershipGeneration == 0 || identity.MembershipGeneration > math.MaxInt64 {
		return false
	}
	if identity.Role != "owner" && identity.Role != "admin" && identity.Role != "member" {
		return false
	}
	return true
}
