package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util"
)

// Autopilot add-on packs (MUL-9487): a one-time purchase of extra autopilot
// runs for the current month, paid on Stripe Checkout. Cloud owns the Price,
// the Checkout Session and the limit; Multica forwards an allowlisted request
// and, once Cloud reports a session paid, makes every API instance read the
// raised limit before it rejects another run.

// maxAutopilotAddonPacks mirrors Cloud's bound on one purchase.
const maxAutopilotAddonPacks = 10

const maxCloudSubscriptionAutopilotAddonIdempotencyKeyLength = 200

type cloudSubscriptionAutopilotAddonCheckoutRequest struct {
	Packs              int    `json:"packs"`
	ExpectedUnitAmount int64  `json:"expected_unit_amount"`
	Currency           string `json:"currency"`
	IdempotencyKey     string `json:"idempotency_key,omitempty"`
}

// cloudSubscriptionAutopilotAddonSessionStatus is the one field Multica reads
// from Cloud's session response; the body is forwarded unchanged.
type cloudSubscriptionAutopilotAddonSessionStatus struct {
	Status string `json:"status"`
}

// CreateCloudWorkspaceAutopilotAddonCheckout forwards a request to pay for
// add-on packs on Stripe Checkout. The allowlisted body names only the pack
// count and the price the caller saw; Cloud repeats the price check, owns
// idempotency and returns the Checkout URL.
func (h *Handler) CreateCloudWorkspaceAutopilotAddonCheckout(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireCloudSubscriptionWorkspace(w, r, "owner", "admin")
	if !ok {
		return
	}
	body, ok := readCloudRuntimeJSONBody(w, r)
	if !ok {
		return
	}
	var in cloudSubscriptionAutopilotAddonCheckoutRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Packs < 1 || in.Packs > maxAutopilotAddonPacks || in.ExpectedUnitAmount < 1 || !isASCIICurrency(in.Currency) {
		writeError(w, http.StatusBadRequest, "invalid autopilot add-on purchase")
		return
	}
	key, ok := requireCloudSubscriptionIdempotencyKey(w, r, in.IdempotencyKey)
	if !ok {
		return
	}
	if len(key) > maxCloudSubscriptionAutopilotAddonIdempotencyKeyLength {
		writeError(w, http.StatusBadRequest, "autopilot add-on idempotency key must be at most 200 bytes")
		return
	}
	in.Currency = strings.ToLower(in.Currency)
	in.IdempotencyKey = key
	upstreamBody, err := json.Marshal(in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build autopilot add-on purchase")
		return
	}
	h.proxyCloudSubscription(w, r, http.MethodPost,
		"/api/v1/subscriptions/"+workspaceID+"/autopilot-addon/checkout-sessions", userID, upstreamBody,
		http.Header{idempotencyKeyHeader: []string{key}})
}

// GetCloudWorkspaceAutopilotAddonCheckout forwards a Checkout Session status
// read. Cloud settles a paid session before answering, so the Billing return
// page learns at once that the runs were added.
func (h *Handler) GetCloudWorkspaceAutopilotAddonCheckout(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requireCloudSubscriptionWorkspace(w, r, "owner", "admin")
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionId")
	if !isValidStripeSessionID(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid session_id")
		return
	}
	h.proxyCloudAutopilotAddonPurchase(w, r, http.MethodGet,
		"/api/v1/subscriptions/"+workspaceID+"/autopilot-addon/checkout-sessions/"+sessionID, workspaceID, userID, nil, nil)
}

func (h *Handler) proxyCloudAutopilotAddonPurchase(
	w http.ResponseWriter,
	r *http.Request,
	method, path, workspaceID, userID string,
	body []byte,
	headers http.Header,
) {
	if h.CloudRuntime == nil || !h.CloudRuntime.Enabled() {
		writeFeatureDisabled(w, "cloud_runtime_not_configured", "cloud runtime is not configured")
		return
	}
	resp, err := h.CloudRuntime.Do(r.Context(), cloudruntime.Request{
		Method:    method,
		Path:      path,
		Body:      body,
		UserID:    userID,
		RequestID: cloudRuntimeRequestID(r),
		Op:        "billing",
		Headers:   headers,
	})
	if err != nil {
		writeCloudRuntimeError(w, r, err)
		return
	}
	if resp.StatusCode == http.StatusOK {
		var status cloudSubscriptionAutopilotAddonSessionStatus
		if json.Unmarshal(resp.Body, &status) == nil && status.Status == "paid" {
			h.recordAutopilotAddonPaid(r, workspaceID)
		}
	}
	writeCloudRuntimeResponse(w, resp)
}

// recordAutopilotAddonPaid refreshes this instance's cached policy so the
// Billing page shows the raised limit at once. Admission does not depend on
// it: every instance fetches the policy from Cloud before rejecting a run.
func (h *Handler) recordAutopilotAddonPaid(r *http.Request, workspaceID string) {
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return
	}
	refresher, ok := h.Entitlements.(entitlement.FreshRefresher)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	refresher.RefreshGateNow(ctx, uuid.UUID(workspaceUUID.Bytes), entitlement.GateAutopilotRuns)
}
