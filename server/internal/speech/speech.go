// Package speech lets the web app stream voice input to a speech-to-text
// provider without the provider's credential ever reaching the browser.
//
// Desktop transcribes on the device. A browser has no local engine, so when
// the operator configures a provider the server mints a short-lived token
// for one streaming session at a time. The token only has to be presented
// when the browser opens the stream: an open stream keeps running until the
// browser closes it, however long that is.
package speech

import (
	"context"
	"os"
	"strings"
)

// Session is what the browser needs to open one streaming session.
type Session struct {
	// Provider names the streaming protocol the app speaks ("deepgram").
	Provider string `json:"provider"`
	// URL is the provider's streaming endpoint with the model and audio
	// format already chosen. The app appends its hotwords.
	URL string `json:"url"`
	// Token authenticates opening the stream. It expires after ExpiresIn
	// seconds, which bounds when the stream can START, not how long it runs.
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// TokenIssuer mints streaming sessions.
type TokenIssuer interface {
	NewSession(ctx context.Context) (Session, error)
}

// FromEnv returns the configured issuer, or nil when voice input on the web is
// not configured. DEEPGRAM_API_KEY turns it on; DEEPGRAM_STT_MODEL picks a
// streaming model other than the default.
func FromEnv() TokenIssuer {
	key := strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY"))
	if key == "" {
		return nil
	}
	return NewDeepgram(DeepgramConfig{
		APIKey: key,
		Model:  strings.TrimSpace(os.Getenv("DEEPGRAM_STT_MODEL")),
	})
}
