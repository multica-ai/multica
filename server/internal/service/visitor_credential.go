package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/requestsecret"
	"github.com/multica-ai/multica/server/internal/util"
)

// VisitorCredentialProvider is the injectable, service-layer boundary that
// yields a short-lived, request-scoped visitor credential for the accountable
// human of a run (WS-11 P1 front half). It is the ONLY place a real upstream
// issuer (Accel / AIME cloud-identity delegation) plugs in. The rest of the
// chain — mint an opaque handle bound to (principal, task), carry it on the
// claim response as RequestSecretRef, resolve it to a single controlled
// child-env key at daemon child-launch, fail-closed on any gap — is already
// implemented in-repo and does not depend on which issuer sits here.
//
// The contract is deliberately narrow and safe by construction:
//   - It receives the SERVER-AUTHENTICATED principal (the run's originator user
//     id, resolved from the trigger chain — never a browser-supplied value) and
//     the task id the credential will be bound to. The browser never supplies a
//     JWT, a store path, or a ref.
//   - It returns the plaintext short-lived secret ONLY to the service, which
//     immediately hands it to requestsecret.Store.Issue and keeps only the
//     opaque handle. The secret never enters the task row, the wire, a log line,
//     or the prompt.
//   - A nil provider, an empty principal, or (ok == false) means "no visitor
//     credential for this run" → no ref is minted → the daemon injects nothing
//     and the downstream query fails closed. There is NO service-account or
//     fixed-account fallback, by design.
//
// A real issuer is wired by setting TaskService.VisitorCredentials in
// cmd/server/router.go (mirroring how TaskService.Composio is wired), exactly
// as the optional Composio overlay hook is. Until an issuer exists, the field
// stays nil and every run is fail-closed — which is the intended default.
type VisitorCredentialProvider interface {
	// VisitorCredential returns the short-lived secret to bind to this run.
	// principalUserID is the authenticated originator (accountable human);
	// taskID is the queued task the handle will be bound to. Return ok=false
	// (with a nil error) to signal "no credential for this principal" — a
	// benign, fail-closed outcome, not an error. Return a non-nil error only
	// for genuine issuer faults; the caller treats both as "no ref" and never
	// falls back to any other identity.
	VisitorCredential(ctx context.Context, principalUserID pgtype.UUID, taskID pgtype.UUID) (secret string, ok bool, err error)
}

// requestSecretStore is the minimal surface of *requestsecret.Store the service
// needs. It exists so tests can inject a stub and so a nil store degrades to
// fail-closed rather than panicking.
type requestSecretStore interface {
	Issue(principalID, taskID, secret string) (string, error)
}

// ResolveRequestSecretRef mints an opaque, one-time, TTL-bound handle for the
// authenticated principal and returns it for storage as the task's
// RequestSecretRef. It is fail-closed in every degraded case: a nil provider,
// nil store, empty principal/task, no credential for the principal, an issuer
// error, or a mint error all yield "" (no ref) — never a partial or fallback
// credential. The plaintext secret is never logged; only non-secret identifiers
// and error text (which the store guarantees carries no secret) are.
//
// The single wiring point that turns this from fail-closed-default into a live
// path is TaskService.VisitorCredentials (nil today). No other change is
// required in the mint→carry→consume chain.
func (s *TaskService) ResolveRequestSecretRef(ctx context.Context, principalUserID pgtype.UUID, taskID pgtype.UUID) string {
	if s == nil || s.VisitorCredentials == nil {
		return ""
	}
	if !principalUserID.Valid || !taskID.Valid {
		// No authenticated principal or no bound task → nothing to mint.
		return ""
	}
	store := s.requestSecrets()
	if store == nil {
		return ""
	}

	secret, ok, err := s.VisitorCredentials.VisitorCredential(ctx, principalUserID, taskID)
	if err != nil {
		slog.Warn("visitor credential: provider returned error; task runs without a request secret (fail-closed)",
			"principal_user_id", util.UUIDToString(principalUserID),
			"task_id", util.UUIDToString(taskID),
			"error", err,
		)
		return ""
	}
	if !ok || secret == "" {
		// Benign: this principal has no visitor credential. Fail-closed.
		return ""
	}

	handle, err := store.Issue(util.UUIDToString(principalUserID), util.UUIDToString(taskID), secret)
	if err != nil {
		// Store.Issue errors never carry the secret (requestsecret guarantees
		// this). Missing binding / empty secret / entropy failure all land here.
		slog.Warn("visitor credential: minting request-secret handle failed; task runs without a request secret (fail-closed)",
			"principal_user_id", util.UUIDToString(principalUserID),
			"task_id", util.UUIDToString(taskID),
			"error", err,
		)
		return ""
	}
	return handle
}

// requestSecrets returns the store the service mints handles into, or nil when
// none is configured (fail-closed). A dedicated accessor keeps the nil-guard in
// one place.
func (s *TaskService) requestSecrets() requestSecretStore {
	if s == nil {
		return nil
	}
	if s.RequestSecrets == nil {
		return nil
	}
	return s.RequestSecrets
}

// ErrNoVisitorCredential is a convenience sentinel a real provider MAY return
// to make "no credential for this principal" explicit; the caller treats it the
// same as ok=false. Providers are equally free to just return ok=false.
var ErrNoVisitorCredential = errors.New("no visitor credential for principal")

// Compile-time assertion that the concrete store satisfies the minimal
// interface the service depends on.
var _ requestSecretStore = (*requestsecret.Store)(nil)
