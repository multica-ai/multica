package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDeepgramAPI    = "https://api.deepgram.com"
	defaultDeepgramStream = "wss://api.deepgram.com/v2/listen"
	// Flux ends a turn when the speaker finishes a thought, so the app can
	// type each finished sentence while it shows the words still coming.
	defaultDeepgramModel = "flux-general-en"
	// The browser fetches the token right before it opens the stream; the
	// only wait in between is the microphone permission prompt, which the
	// app answers before it asks for a token.
	deepgramTokenTTL = 30
	// The app sends 16 kHz mono PCM.
	deepgramSampleRate = 16000
	maxResponseBytes   = 64 << 10
)

// DeepgramConfig configures the Deepgram issuer. Empty fields take the
// defaults: the public API and the flux-general-en model.
type DeepgramConfig struct {
	APIKey string
	Model  string
	// APIURL and StreamURL point at another Deepgram endpoint (tests).
	APIURL     string
	StreamURL  string
	HTTPClient *http.Client
}

// Deepgram mints Deepgram grant tokens for its streaming API.
type Deepgram struct {
	key       string
	model     string
	apiURL    string
	streamURL string
	client    *http.Client
}

func NewDeepgram(cfg DeepgramConfig) *Deepgram {
	d := &Deepgram{
		key:       cfg.APIKey,
		model:     cfg.Model,
		apiURL:    strings.TrimRight(cfg.APIURL, "/"),
		streamURL: cfg.StreamURL,
		client:    cfg.HTTPClient,
	}
	if d.model == "" {
		d.model = defaultDeepgramModel
	}
	if d.apiURL == "" {
		d.apiURL = defaultDeepgramAPI
	}
	if d.streamURL == "" {
		d.streamURL = defaultDeepgramStream
	}
	if d.client == nil {
		d.client = &http.Client{Timeout: 10 * time.Second}
	}
	return d
}

func (d *Deepgram) NewSession(ctx context.Context) (Session, error) {
	body, _ := json.Marshal(map[string]int{"ttl_seconds": deepgramTokenTTL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.apiURL+"/v1/auth/grant", bytes.NewReader(body))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Authorization", "Token "+d.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("deepgram grant: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Session{}, fmt.Errorf("deepgram grant: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Deepgram's error bodies describe the request, never the key.
		return Session{}, fmt.Errorf("deepgram grant: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	var out struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return Session{}, fmt.Errorf("deepgram grant: no access_token in response")
	}
	expiresIn := int(out.ExpiresIn)
	if expiresIn <= 0 {
		expiresIn = deepgramTokenTTL
	}

	q := url.Values{}
	q.Set("model", d.model)
	q.Set("encoding", "linear16")
	q.Set("sample_rate", fmt.Sprint(deepgramSampleRate))
	return Session{
		Provider:  "deepgram",
		URL:       d.streamURL + "?" + q.Encode(),
		Token:     out.AccessToken,
		ExpiresIn: expiresIn,
	}, nil
}

func snippet(body []byte) string {
	const max = 300
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
