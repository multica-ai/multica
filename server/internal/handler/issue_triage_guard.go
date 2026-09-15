package handler

import "net/http"

// Write protection for the reserved `triage` status key (MUL-7189 §2.2).
//
// Triage itself is NOT a status — it lives in issue.triage_state — so there is
// no "issue in Triage is read-only" rule to enforce here. Status, project and
// parent on a Triage entry are the triager's proposal, and accept is what
// confirms them; locking them would only stop the triager from doing its job.
//
// What remains is the key: `triage` cannot be a custom status. The catalog
// carries a CHECK (migration 475) and issuestatus.Resolve refuses the key, so
// every create / update / batch path is covered by the one resolver. This is
// the response that refusal renders.
func writeStatusReservedForTriage(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusBadRequest, "status_reserved_for_triage",
		`status "triage" is reserved: Triage is not a status, so no issue can be moved into or out of it by a status write`)
}
