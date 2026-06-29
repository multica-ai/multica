package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/analytics"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/sourcebeacon"
)

// HandleSelfHostSourceBeacon ingests the self-host onboarding source beacon
// (MUL-3708). It is mounted PUBLIC and unauthenticated by design: self-host
// instances hold no Multica credential, so the data is treated as a coarse
// directional signal, not a trusted count. Abuse is bounded by the per-IP
// rate limiter (router), the tiny body cap, the channel allowlist, and
// strict unknown-field rejection.
//
// The payload is anonymous by construction — only channel enums plus two
// per-instance hashes. DisallowUnknownFields means any stray identity field
// (email, source_other, …) is rejected rather than logged or forwarded.
//
// Each valid channel becomes one PostHog event with a deterministic uuid
// (PostHog dedups on uuid) and $process_person_profile=false (so the
// anonymous uid_hash never spawns a PostHog person). On the official cloud
// this lands in the configured PostHog; on a self-host instance the same
// route exists but is unused (its analytics client is a no-op).
func (h *Handler) HandleSelfHostSourceBeacon(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, sourcebeacon.MaxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var p sourcebeacon.Payload
	if err := dec.Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Reject trailing data / a second JSON value in the body.
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if p.V != sourcebeacon.SchemaVersion {
		writeError(w, http.StatusBadRequest, "unsupported schema version")
		return
	}
	if !sourcebeacon.IsValidHash(p.UIDHash) || !sourcebeacon.IsValidHash(p.InstanceHash) {
		writeError(w, http.StatusBadRequest, "invalid hash")
		return
	}

	channels := sourcebeacon.FilterValidChannels(p.Channels)
	if len(channels) == 0 {
		writeError(w, http.StatusBadRequest, "no valid channels")
		return
	}

	for _, ch := range channels {
		ev := analytics.SelfHostSourceChannel(
			p.InstanceHash,
			p.UIDHash,
			ch,
			sourcebeacon.EventUUID(p.InstanceHash, p.UIDHash, ch),
		)
		// RecordEvent (not a naked Capture) so the event also increments the
		// Prometheus counter via IncForEvent. Not IsMetricsOnly, so it still
		// ships to PostHog. m==nil-safe (tests / metrics-disabled deploys).
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, ev)
	}

	w.WriteHeader(http.StatusNoContent)
}
