package handler

import (
	"net/http"
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
	case "task_token", "cloud_pat":
		return true
	default:
		return false
	}
}
