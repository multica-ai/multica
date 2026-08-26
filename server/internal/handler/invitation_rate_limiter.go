package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/redis/go-redis/v9"
)

const (
	invitationActorLimiterKeyPrefix     = "mul:invitation:actor:"
	invitationWorkspaceLimiterKeyPrefix = "mul:invitation:workspace:"
	invitationRecipientLimiterKeyPrefix = "mul:invitation:recipient:"
)

// InvitationRateLimits configures the independent invitation admission gates.
type InvitationRateLimits struct {
	Actor     SlidingWindowRateLimit
	Workspace SlidingWindowRateLimit
	Recipient SlidingWindowRateLimit
}

// DefaultInvitationRateLimits returns conservative email-abuse budgets.
func DefaultInvitationRateLimits() InvitationRateLimits {
	return InvitationRateLimits{
		Actor:     SlidingWindowRateLimit{Limit: 10, Window: 10 * time.Minute},
		Workspace: SlidingWindowRateLimit{Limit: 50, Window: 24 * time.Hour},
		Recipient: SlidingWindowRateLimit{Limit: 6, Window: 24 * time.Hour},
	}
}

// InvitationRateLimiters holds the independently keyed gate implementations.
type InvitationRateLimiters struct {
	Actor     SlidingWindowRateLimiter
	Workspace SlidingWindowRateLimiter
	Recipient SlidingWindowRateLimiter
}

type invitationRateLimitDimension string

const (
	invitationRateLimitActor     invitationRateLimitDimension = "actor"
	invitationRateLimitWorkspace invitationRateLimitDimension = "workspace"
	invitationRateLimitRecipient invitationRateLimitDimension = "recipient"
)

// NewMemoryInvitationRateLimiters builds single-process gates for local and
// Redis-less deployments.
func NewMemoryInvitationRateLimiters(limits InvitationRateLimits) InvitationRateLimiters {
	return InvitationRateLimiters{
		Actor:     newMemorySlidingWindowRateLimiter(limits.Actor),
		Workspace: newMemorySlidingWindowRateLimiter(limits.Workspace),
		Recipient: newMemorySlidingWindowRateLimiter(limits.Recipient),
	}
}

// NewRedisInvitationRateLimiters builds replica-shared gates with isolated key
// namespaces for each dimension.
func NewRedisInvitationRateLimiters(rdb *redis.Client, limits InvitationRateLimits) InvitationRateLimiters {
	return InvitationRateLimiters{
		Actor:     newRedisSlidingWindowRateLimiter(rdb, limits.Actor, invitationActorLimiterKeyPrefix),
		Workspace: newRedisSlidingWindowRateLimiter(rdb, limits.Workspace, invitationWorkspaceLimiterKeyPrefix),
		Recipient: newRedisSlidingWindowRateLimiter(rdb, limits.Recipient, invitationRecipientLimiterKeyPrefix),
	}
}

type invitationRateLimitGate struct {
	dimension invitationRateLimitDimension
	key       string
	limiter   SlidingWindowRateLimiter
}

type invitationAdmission struct {
	gates []invitationRateLimitGate
}

func invitationRecipientKey(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:])
}

func (h *Handler) checkInvitationAdmission(w http.ResponseWriter, r *http.Request, actorID, workspaceID, email string) (*invitationAdmission, bool) {
	gates := []invitationRateLimitGate{
		{dimension: invitationRateLimitActor, key: actorID, limiter: h.InvitationRateLimiters.Actor},
		{dimension: invitationRateLimitWorkspace, key: workspaceID, limiter: h.InvitationRateLimiters.Workspace},
		{dimension: invitationRateLimitRecipient, key: invitationRecipientKey(email), limiter: h.InvitationRateLimiters.Recipient},
	}

	var checkErr error
	var checkErrorGates []string
	var deniedGates []invitationRateLimitGate
	var retryAfter time.Duration
	for _, gate := range gates {
		allowed, err := slidingWindowLimiterCheckWithError(r.Context(), gate.limiter, gate.key)
		if err != nil {
			if checkErr == nil {
				checkErr = err
			}
			checkErrorGates = append(checkErrorGates, string(gate.dimension))
			continue
		}
		if allowed {
			continue
		}

		deniedGates = append(deniedGates, gate)
		if retry := slidingWindowLimiterRetryAfter(r.Context(), gate.limiter, gate.key); retry > retryAfter {
			retryAfter = retry
		}
	}

	if checkErr != nil {
		slog.Error("invitation rate limiter unavailable", append(logger.RequestAttrs(r), "gates", strings.Join(checkErrorGates, ","), "error", checkErr)...)
		writeInvitationLimiterUnavailable(w)
		return nil, false
	}
	if len(deniedGates) > 0 {
		for _, gate := range deniedGates {
			h.Metrics.RecordEmailRateLimited("workspace_invitation", string(gate.dimension))
		}
		writeInvitationRateLimited(w, retryAfter)
		return nil, false
	}
	return &invitationAdmission{gates: gates}, true
}

func (h *Handler) consumeInvitationAdmission(r *http.Request, admission *invitationAdmission) {
	if admission == nil {
		return
	}
	h.consumeInvitationGates(r, admission.gates)
}

// consumeInvitationActorAdmission charges persistent capacity rejections to
// the caller without spending the shared workspace or recipient email budgets.
func (h *Handler) consumeInvitationActorAdmission(r *http.Request, admission *invitationAdmission) {
	if admission == nil {
		return
	}
	for _, gate := range admission.gates {
		if gate.dimension == invitationRateLimitActor {
			h.consumeInvitationGates(r, []invitationRateLimitGate{gate})
			return
		}
	}
}

func (h *Handler) consumeInvitationGates(r *http.Request, gates []invitationRateLimitGate) {
	for _, gate := range gates {
		allowed, err := slidingWindowLimiterAllow(r.Context(), gate.limiter, gate.key)
		if err != nil {
			// All gates passed the non-consuming checks, so this failure began in
			// the narrow Check-to-Allow window. Admit this in-flight request rather
			// than return an error after an earlier gate may have consumed. A
			// sustained backend failure is caught by Phase 1 on later requests.
			slog.Warn("invitation rate limiter consume failed after successful checks; allowing bounded overshoot", append(logger.RequestAttrs(r), "gate", string(gate.dimension), "error", err)...)
			continue
		}
		if !allowed {
			// Another replica filled this gate between Check and Allow. Continue
			// instead of rejecting after earlier gates may already have consumed;
			// this permits only a bounded overshoot of a safety budget.
			continue
		}
	}
}

func writeInvitationLimiterUnavailable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "5")
	writeErrorCode(w, http.StatusServiceUnavailable, "invitation_rate_limiter_unavailable", "invitation service temporarily unavailable")
}

func writeInvitationRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	retryAfterSeconds := int64((retryAfter + time.Second - 1) / time.Second)
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "too many invitation requests",
		"code":                "invitation_rate_limited",
		"retry_after_seconds": retryAfterSeconds,
	})
}
