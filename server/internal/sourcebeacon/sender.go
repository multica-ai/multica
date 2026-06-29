package sourcebeacon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultUpstreamURL is Multica's public API base; the beacon path is
	// appended. Overridable via MULTICA_SOURCE_BEACON_URL.
	defaultUpstreamURL = "https://api.multica.ai"
	beaconPath         = "/api/telemetry/self-host-source"
	sendTimeout        = 5 * time.Second
)

// Sender ships the onboarding source beacon from a production self-host
// server. A disabled or salt-less Sender is a silent no-op. The zero value
// is not usable; build one with NewSender.
type Sender struct {
	enabled    bool
	salt       string
	endpoint   string
	httpClient *http.Client
}

// SenderConfig configures NewSender.
type SenderConfig struct {
	// Enabled is the ShouldSend decision for this deployment. Typically
	// sourcebeacon.ShouldSendFromEnv().
	Enabled bool
	// Salt is the per-instance secret (system_settings.instance_salt). When
	// empty the Sender is forced off — we never ship un-salted hashes.
	Salt string
	// UpstreamURL overrides the public API base (default api.multica.ai).
	UpstreamURL string
	// HTTPClient overrides the default short-timeout client (tests inject a
	// stub).
	HTTPClient *http.Client
}

// NewSender returns a configured Sender. It is enabled only when both
// cfg.Enabled and a non-empty salt are present.
func NewSender(cfg SenderConfig) *Sender {
	base := strings.TrimRight(strings.TrimSpace(cfg.UpstreamURL), "/")
	if base == "" {
		base = defaultUpstreamURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: sendTimeout}
	}
	return &Sender{
		enabled:    cfg.Enabled && strings.TrimSpace(cfg.Salt) != "",
		salt:       cfg.Salt,
		endpoint:   base + beaconPath,
		httpClient: hc,
	}
}

// Enabled reports whether this Sender will actually ship beacons. Nil-safe
// so the /api/config notice flag and the onboarding hook can call it
// without guarding.
func (s *Sender) Enabled() bool {
	return s != nil && s.enabled
}

// MaybeSend fires one fire-and-forget beacon for the user's selected
// channels. It is a no-op when disabled, when userID is empty, or when no
// channel is valid. It never blocks the caller and never surfaces an error
// — onboarding must not depend on telemetry succeeding.
func (s *Sender) MaybeSend(userID string, channels []string) {
	if !s.Enabled() || strings.TrimSpace(userID) == "" {
		return
	}
	valid := FilterValidChannels(channels)
	if len(valid) == 0 {
		return
	}
	payload := Payload{
		V:            SchemaVersion,
		Channels:     valid,
		UIDHash:      HashUID(s.salt, userID),
		InstanceHash: HashInstance(s.salt),
	}
	go s.post(payload)
}

func (s *Sender) post(payload Payload) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("sourcebeacon: marshal payload failed", "error", err)
		return
	}
	// Detached context: the originating request has already returned, so we
	// must not inherit its (now-cancelled) context.
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		slog.Warn("sourcebeacon: build request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// Loss is acceptable for this coarse signal; log at debug so a flaky
		// network doesn't spam self-host logs.
		slog.Debug("sourcebeacon: send failed (ignored)", "error", err)
		return
	}
	_ = resp.Body.Close()
}
