package sourcechannel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	ReportPath     = "/api/acquisition/self-host-source"
	systemSaltKey  = "self_host_source_channel_salt"
	defaultTimeout = 3 * time.Second
)

type settingStore interface {
	GetOrCreateSystemSetting(ctx context.Context, arg db.GetOrCreateSystemSettingParams) (string, error)
}

type SenderConfig struct {
	HTTPClient *http.Client
	Timeout    time.Duration
	Logger     *slog.Logger
}

type Sender struct {
	settings settingStore
	client   *http.Client
	endpoint string
	timeout  time.Duration
	logger   *slog.Logger

	mu   sync.RWMutex
	salt string
}

func NewSender(settings settingStore, cfg SenderConfig) (*Sender, error) {
	if settings == nil {
		return nil, errors.New("sourcechannel: settings store is nil")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Sender{
		settings: settings,
		client:   client,
		endpoint: OfficialMulticaAPIURL + ReportPath,
		timeout:  timeout,
		logger:   logger,
	}, nil
}

func MustNewSender(settings settingStore, cfg SenderConfig) *Sender {
	s, err := NewSender(settings, cfg)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *Sender) ReportSelfHostSourceChannel(userID, channel, sourceOther, apiBaseURL string, includeDomain bool) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	channel = NormalizeChannel(channel)
	if userID == "" || !ValidChannel(channel) {
		return
	}
	domain := NormalizeDomain(apiBaseURL)
	if !ShouldReportAPIBaseURL(apiBaseURL) {
		return
	}
	sourceOther = NormalizeSourceOther(channel, sourceOther)
	go s.report(context.Background(), userID, channel, sourceOther, domain, includeDomain)
}

func (s *Sender) report(parent context.Context, userID, channel, sourceOther, domain string, includeDomain bool) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	salt, err := s.instanceSalt(ctx)
	if err != nil {
		s.logger.Debug("self-host source channel: salt unavailable", "error", err)
		return
	}

	var plainDomain *string
	if includeDomain {
		plainDomain = &domain
	}
	payload := Report{
		SchemaVersion: SchemaVersion,
		Channel:       channel,
		InstanceHash:  InstanceHash(salt),
		SubjectHash:   SubjectHash(salt, userID),
		SourceOther:   sourceOther,
		Domain:        plainDomain,
		DomainMD5:     DomainMD5(domain),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Debug("self-host source channel: encode failed", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		s.logger.Debug("self-host source channel: request build failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "multica-self-host-source/1")

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Debug("self-host source channel: post failed", "error", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.logger.Debug("self-host source channel: non-2xx response", "status", resp.StatusCode)
	}
}

func (s *Sender) instanceSalt(ctx context.Context) (string, error) {
	s.mu.RLock()
	if s.salt != "" {
		defer s.mu.RUnlock()
		return s.salt, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.salt != "" {
		return s.salt, nil
	}
	candidate, err := randomSalt()
	if err != nil {
		return "", err
	}
	salt, err := s.settings.GetOrCreateSystemSetting(ctx, db.GetOrCreateSystemSettingParams{
		Key:   systemSaltKey,
		Value: candidate,
	})
	if err != nil {
		return "", err
	}
	s.salt = strings.TrimSpace(salt)
	return s.salt, nil
}

func randomSalt() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
