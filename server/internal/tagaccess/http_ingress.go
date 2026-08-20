package tagaccess

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxAuthorityEnvelopeBytes = 4 << 20

// HTTPIngress is the private service transport for already-authenticated VIBES
// authority envelopes. It exposes no read, debug, grant, or native-auth path.
type HTTPIngress struct {
	access *AuthenticatedAccess
}

type workspaceHTTPReceipt struct {
	Source string `json:"source"`
	TwoStageReceipt
}

type identityHTTPReceipt struct {
	Source string `json:"source"`
	IdentityTwoStageReceipt
}

type sessionWorkspaceHTTPReceipt struct {
	Source string `json:"source"`
	SessionWorkspaceTwoStageReceipt
}

func NewHTTPIngress(access *AuthenticatedAccess) (*HTTPIngress, error) {
	if access == nil || access.Ingress == nil || access.IdentityIngress == nil || access.SessionWorkspaceIngress == nil {
		return nil, errors.New("Tag authority HTTP ingress requires authenticated access")
	}
	return &HTTPIngress{access: access}, nil
}

func (h *HTTPIngress) SessionWorkspace(w http.ResponseWriter, r *http.Request) {
	var envelope SessionWorkspaceSupersededEnvelope
	if err := decodeAuthorityRequest(w, r, &envelope); err != nil {
		writeAuthorityJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	receipt, err := h.access.SessionWorkspaceIngress.Deliver(r.Context(), envelope)
	if err != nil {
		authorityError(w, err)
		return
	}
	writeAuthorityJSON(w, http.StatusOK, sessionWorkspaceHTTPReceipt{Source: "session_workspace", SessionWorkspaceTwoStageReceipt: receipt})
}

func decodeAuthorityRequest(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthorityEnvelopeBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("multiple JSON documents")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeAuthorityJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func authorityError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	code := "authority_store_unavailable"
	if errors.Is(err, ErrUnverifiedDelivery) {
		status, code = http.StatusUnauthorized, "unverified_delivery"
	} else if errors.Is(err, ErrInvalidProjection) {
		status, code = http.StatusBadRequest, "invalid_delivery"
	}
	writeAuthorityJSON(w, status, map[string]string{"error": code})
}

func (h *HTTPIngress) Workspace(w http.ResponseWriter, r *http.Request) {
	var envelope AuthorityEnvelope
	if err := decodeAuthorityRequest(w, r, &envelope); err != nil {
		writeAuthorityJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	receipt, err := h.access.Ingress.Deliver(r.Context(), envelope)
	if err != nil {
		authorityError(w, err)
		return
	}
	writeAuthorityJSON(w, http.StatusOK, workspaceHTTPReceipt{Source: "workspace", TwoStageReceipt: receipt})
}

func (h *HTTPIngress) Identity(w http.ResponseWriter, r *http.Request) {
	var envelope IdentityRestrictionEnvelope
	if err := decodeAuthorityRequest(w, r, &envelope); err != nil {
		writeAuthorityJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	receipt, err := h.access.IdentityIngress.Deliver(r.Context(), envelope)
	if err != nil {
		authorityError(w, err)
		return
	}
	writeAuthorityJSON(w, http.StatusOK, identityHTTPReceipt{Source: "identity", IdentityTwoStageReceipt: receipt})
}
