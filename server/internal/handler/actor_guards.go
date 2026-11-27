package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// RequireHumanActor rejects machine credentials on human-only route groups.
// Auth strips client-supplied X-Actor-Source and stamps task_token or cloud_pat;
// that authoritative header distinguishes a machine acting for its human owner.
// resolveActor classifies workspace authorship and is not this authorization gate.
//
// Attach this middleware only to human-only scopes such as billing. Issue and
// chat routes permit authorized agent traffic. Classify new auth sources here
// when adding them; unknown values currently remain human-equivalent.
func RequireHumanActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMachineCredentialActor(r) {
			writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isMachineCredentialActor centralizes the authoritative actor-source check so
// sensitive handlers can keep a fail-closed backstop in addition to their
// router middleware. Unknown actor sources intentionally remain human-equivalent
// until their authentication branch is explicitly classified here.
func isMachineCredentialActor(r *http.Request) bool {
	// X-Actor-Source is server-set only. The auth middleware strips any
	// client-supplied value before stamping its own, so a recognized value here
	// is authoritative.
	switch r.Header.Get("X-Actor-Source") {
	case "task_token", "cloud_pat", middleware.ActorSourceServiceAccount:
		return true
	default:
		return false
	}
}

// RequireUserAccount rejects service accounts on user-account routes.
//
// Service accounts (user.kind='service') are machine identities that belong to
// exactly one workspace. Unlike task tokens and cloud PATs — which represent a
// machine acting for its human owner — they should never reach the routes that
// bootstrap a human's presence in the system: minting their own credentials,
// accepting invitations, joining via share links, or creating workspaces. Their
// access comes entirely from the membership created alongside them.
//
// The Auth middleware stamps X-Actor-Source: service_account on mul_ PATs
// issued to service users (both PAT cache hit and miss paths), so this check is
// authoritative the same way RequireHumanActor's is. JWT sessions cannot exist
// for service accounts: VerifyCode refuses kind='service' rows, so the only
// credential is the PAT, which is always stamped.
//
// Attach this middleware only to account-level scopes. Workspace-scoped
// authorization (membership) is unchanged — a service account acts as a member
// of its one workspace through the ordinary member path.
func RequireUserAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Actor-Source") == middleware.ActorSourceServiceAccount {
			writeError(w, http.StatusForbidden, "this endpoint is not available to service accounts")
			return
		}
		next.ServeHTTP(w, r)
	})
}
