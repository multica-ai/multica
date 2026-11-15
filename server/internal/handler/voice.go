package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// Each dictation mints one session. The per-user budget bounds how many
// grants one account (or a leaked credential of it) can mint. It does not
// bound the streams themselves: a grant opens any number of streams during
// its 30 seconds, each running until closed. That is the contract of
// Deepgram grant tokens, accepted here because only signed-in members can
// mint; bounding stream time would take a server-side stream proxy.
const (
	voiceSessionsPerWindow = 30
	voiceSessionWindow     = time.Minute
)

const voiceSessionLimiterKeyPrefix = "mul:voice:session:"

func voiceSessionLimit() SlidingWindowRateLimit {
	return SlidingWindowRateLimit{Limit: voiceSessionsPerWindow, Window: voiceSessionWindow}
}

// NewMemoryVoiceSessionLimiter bounds voice-session minting per user on one
// replica (local and Redis-less deployments).
func NewMemoryVoiceSessionLimiter() SlidingWindowRateLimiter {
	return newMemorySlidingWindowRateLimiter(voiceSessionLimit())
}

// NewRedisVoiceSessionLimiter shares the per-user budget across replicas.
func NewRedisVoiceSessionLimiter(rdb redis.UniversalClient) SlidingWindowRateLimiter {
	return newRedisSlidingWindowRateLimiter(rdb, voiceSessionLimit(), voiceSessionLimiterKeyPrefix)
}

// CreateVoiceSession mints a short-lived token for one voice-input stream
// from the web app. The provider's API key stays on the server; the token
// only has to be presented when the browser opens the stream, which then
// runs until the browser closes it. Returns 404 when the operator has not
// configured a speech provider; /api/config then omits
// voice_transcription_enabled and the web app shows no mic.
func (h *Handler) CreateVoiceSession(w http.ResponseWriter, r *http.Request) {
	if h.Speech == nil {
		writeError(w, http.StatusNotFound, "voice input is not configured on this server")
		return
	}
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "sign in to use voice input")
		return
	}
	allowed, err := slidingWindowLimiterAllow(r.Context(), h.VoiceSessionLimiter, userID)
	if err != nil || !allowed {
		writeError(w, http.StatusTooManyRequests, "too many dictations started; wait a minute")
		return
	}
	session, err := h.Speech.NewSession(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "voice input: speech provider refused a session", "error", err)
		writeError(w, http.StatusBadGateway, "the speech provider could not be reached")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, session)
}
