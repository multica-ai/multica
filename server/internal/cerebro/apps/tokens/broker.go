// Package tokens is the thin trust boundary between Cerebro apps and the
// Firtal Data Registry. It exchanges the operator credential for an app-bound
// personal session key; app processes never receive the operator credential.
package tokens

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	exchangePath  = "/api/registry/v1/sessions/exchange"
	aiProxyPath   = "/api/ai/proxy/v1"
	defaultTTL    = 3600
	expirySkew    = 60 * time.Second
	responseLimit = 64 << 10
)

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`)
	accessValues  = map[string]bool{"read": true, "write": true, "read_write": true}
	resourceTypes = map[string]bool{
		"data_source": true, "data_destination": true, "app": true,
		"function": true, "integration": true, "bigquery_credential": true,
	}
)

type Scope struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Access       string `json:"access"`
}

type AppGrant struct {
	ID      string  `json:"id"`
	Version string  `json:"version"`
	Scopes  []Scope `json:"scopes"`
}

type Identity struct {
	MemberID string
	App      AppGrant
}

type Token struct {
	Key       string    `json:"key"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
	// AIBaseURL is the gateway the app must call with this key. Apps never hard
	// code an environment: the broker hands out the base URL that belongs to the
	// registry the key was exchanged against.
	AIBaseURL string `json:"ai_base_url"`
}

type Config struct {
	BaseURL    string
	SystemKey  string
	TTLSeconds int
	Timeout    time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		BaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("FIRTAL_REGISTRY_URL")), "/"),
		SystemKey: strings.TrimSpace(os.Getenv("FIRTAL_REGISTRY_KEY")),
	}
}

type Broker struct {
	config Config
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	cache  map[string]Token
}

func NewBroker(config Config, client *http.Client) *Broker {
	if config.TTLSeconds <= 0 {
		config.TTLSeconds = defaultTTL
	}
	if config.Timeout <= 0 {
		config.Timeout = 15 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	return &Broker{config: config, client: client, now: time.Now, cache: make(map[string]Token)}
}

// PersonalKey returns an app-scoped key for one human. The broker deliberately
// has no method that exposes Config.SystemKey.
func (b *Broker) PersonalKey(ctx context.Context, identity Identity) (Token, error) {
	if err := b.validate(identity); err != nil {
		return Token{}, err
	}
	cacheKey, err := identityCacheKey(identity)
	if err != nil {
		return Token{}, fmt.Errorf("app token: cache identity: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if cached, ok := b.cache[cacheKey]; ok && cached.ExpiresAt.After(b.now().Add(expirySkew)) {
		return cached, nil
	}
	token, err := b.exchange(ctx, identity)
	if err != nil {
		return Token{}, err
	}
	b.cache[cacheKey] = token
	return token, nil
}

// Forget removes cached keys matching the complete identity. Registry keys are
// short-lived and expire independently; this prevents reuse immediately in
// Cerebro and forces the next call through exchange again.
func (b *Broker) Forget(identity Identity) int {
	key, err := identityCacheKey(identity)
	if err != nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.cache[key]; !ok {
		return 0
	}
	delete(b.cache, key)
	return 1
}

func (b *Broker) validate(identity Identity) error {
	if b == nil {
		return errors.New("app token broker is not configured")
	}
	baseURL := strings.TrimSpace(b.config.BaseURL)
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return errors.New("app token exchange unavailable: FIRTAL_REGISTRY_URL is not configured")
	}
	if strings.TrimSpace(b.config.SystemKey) == "" {
		return errors.New("app token exchange unavailable: FIRTAL_REGISTRY_KEY is not configured")
	}
	if !uuidPattern.MatchString(strings.ToLower(identity.MemberID)) {
		return errors.New("app token identity requires a valid member id")
	}
	if !uuidPattern.MatchString(strings.ToLower(identity.App.ID)) {
		return errors.New("app token identity requires a valid app id")
	}
	if !semverPattern.MatchString(identity.App.Version) {
		return errors.New("app token identity requires a semantic app version")
	}
	if len(identity.App.Scopes) == 0 {
		return errors.New("app token identity requires approved scopes")
	}
	for _, scope := range identity.App.Scopes {
		if !resourceTypes[scope.ResourceType] || strings.TrimSpace(scope.ResourceID) == "" || !accessValues[scope.Access] {
			return errors.New("app token identity contains an invalid approved scope")
		}
	}
	return nil
}

func (b *Broker) exchange(ctx context.Context, identity Identity) (Token, error) {
	payload, err := json.Marshal(map[string]any{
		"principal":  "member:" + identity.MemberID,
		"ttlSeconds": b.config.TTLSeconds,
		"via_app":    identity.App,
	})
	if err != nil {
		return Token{}, fmt.Errorf("app token exchange: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.config.BaseURL, "/")+exchangePath, bytes.NewReader(payload))
	if err != nil {
		return Token{}, fmt.Errorf("app token exchange: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.config.SystemKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("app token exchange: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	if err != nil {
		return Token{}, fmt.Errorf("app token exchange: read response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		message := strings.ReplaceAll(strings.TrimSpace(string(body)), b.config.SystemKey, "[REDACTED]")
		return Token{}, fmt.Errorf("app token exchange: HTTP %d: %s", resp.StatusCode, message)
	}
	var response struct {
		Key       string `json:"key"`
		ExpiresAt string `json:"expires_at"`
		Session   struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Token{}, fmt.Errorf("app token exchange: decode response: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, response.ExpiresAt)
	if err != nil || response.Key == "" || response.Session.ID == "" {
		return Token{}, errors.New("app token exchange: response is missing key, session id, or expiry")
	}
	return Token{
		Key:       response.Key,
		SessionID: response.Session.ID,
		ExpiresAt: expiresAt,
		AIBaseURL: strings.TrimRight(b.config.BaseURL, "/") + aiProxyPath,
	}, nil
}

func identityCacheKey(identity Identity) (string, error) {
	canonical := identity
	canonical.App.Scopes = append([]Scope(nil), identity.App.Scopes...)
	sort.Slice(canonical.App.Scopes, func(i, j int) bool {
		a, b := canonical.App.Scopes[i], canonical.App.Scopes[j]
		return a.ResourceType+a.ResourceID+a.Access < b.ResourceType+b.ResourceID+b.Access
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ParseWorkflowIdentityEnvelope converts the durable workflow-run envelope to
// the same identity contract used for interactive app sessions.
func ParseWorkflowIdentityEnvelope(raw []byte) (Identity, error) {
	var envelope struct {
		Principal struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"principal"`
		App AppGrant `json:"app"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Identity{}, fmt.Errorf("workflow identity envelope: %w", err)
	}
	if envelope.Principal.Type != "member" {
		return Identity{}, errors.New("workflow identity envelope must carry a member principal")
	}
	return Identity{MemberID: envelope.Principal.ID, App: envelope.App}, nil
}
