package webhooks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxPayloadSize caps the bytes the router will buffer before refusing
// a webhook (413). GitHub's documented webhook payload ceiling is 25
// MB; we set ours at 1 MB which is well above every event type the
// cascade cares about and prevents a misbehaving caller from forcing
// us to allocate large buffers under a 200-in-1s SLO. PR3 may raise
// this for specific GitHub events if needed, but the default is
// deliberately conservative.
const maxPayloadSize = 1 << 20 // 1 MiB

// Router accepts incoming webhooks at POST /webhooks/{source}. It
// validates HMAC, hands the request to the per-source adapter for
// normalization, and reports outcome. PR2 stops there — persistence
// to cascade_retrigger lives in PR3 — but the response codes are
// already the production contract (200 / 202 / 204 / 400 / 401 /
// 404 / 413 / 500) so upstream vendors can wire their delivery
// retries against them today.
type Router struct {
	// sources is a name→adapter registry populated by Register. We
	// keep it unexported and only mutate it through Register so the
	// duplicate-registration panic stays as the only failure mode.
	sources map[string]Source

	// now is injected for tests so ReceivedAt is deterministic. nil
	// means use time.Now (production).
	now func() time.Time

	// logger is the structured logger the router uses. Defaults to
	// slog.Default() when nil.
	logger *slog.Logger
}

// NewRouter constructs an empty Router. Call Register for each Source
// you want to expose, then Mount the router onto a parent Chi router.
func NewRouter(logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		sources: make(map[string]Source),
		logger:  logger,
	}
}

// Register adds a Source to the router. Panics on:
//   - duplicate Name (operator bug, fail loud at startup),
//   - empty Name,
//   - a non-empty SignatureHeader paired with an empty current secret
//     (config error: secret missing from env).
// These all surface at startup, never at request time.
func (r *Router) Register(s Source) {
	name := s.Name()
	if name == "" {
		panic("webhooks: Source.Name() must be non-empty")
	}
	if _, dup := r.sources[name]; dup {
		panic(fmt.Sprintf("webhooks: duplicate Source registration for %q", name))
	}
	if header := s.SignatureHeader(); header != "" {
		current, _ := s.Secrets()
		if current == "" {
			panic(fmt.Sprintf("webhooks: Source %q declares signature header %q but has no current secret configured", name, header))
		}
	}
	r.sources[name] = s
}

// Mount attaches POST /webhooks/{source} to the supplied Chi router.
// Callers wire this from cmd/server/router.go behind the
// MULTICA_CASCADE_WEBHOOK_ENABLED feature flag.
func (r *Router) Mount(parent chi.Router) {
	parent.Post("/webhooks/{source}", r.serve)
}

// SourceCount returns how many Sources are registered. Exists so the
// startup log line and the feature-flag-off test can assert on the
// state without exposing the internal map.
func (r *Router) SourceCount() int {
	return len(r.sources)
}

func (r *Router) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// serve implements the actual HTTP handler. Kept small and linear so
// the 200-in-1s SLO is easy to read: read body (bounded) → HMAC →
// Normalize → log + status. No DB calls, no GitHub API calls, no
// network IO of any kind. PR3 will add an INSERT to cascade_retrigger
// at the end; even that is one bounded write to a local Postgres so
// the SLO holds.
func (r *Router) serve(w http.ResponseWriter, req *http.Request) {
	sourceName := chi.URLParam(req, "source")
	source, ok := r.sources[sourceName]
	if !ok {
		// Unknown source name is a 404, not a 400 — vendors checking
		// our endpoint health by hitting /webhooks/themselves should
		// get a clear "not configured here" rather than a generic
		// validation failure.
		http.NotFound(w, req)
		return
	}

	// Read+buffer the body so we can both HMAC-verify it and let the
	// adapter Normalize it. http.MaxBytesReader caps the read so a
	// caller can't force unbounded allocation; on overflow it returns
	// an error that we translate to 413.
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxPayloadSize))
	if err != nil {
		// MaxBytesError is the only documented error that means
		// "client tried to send too much". Any other read error is
		// also unrecoverable from this request's perspective; both
		// surface as the same status because we cannot distinguish
		// "too large" from "transport hiccup at byte N+1" reliably
		// without the typed error, and both are caller-fixable
		// (lower payload size or retry).
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			r.logger.Warn("webhooks.payload_too_large",
				"source", sourceName,
				"limit_bytes", maxPayloadSize,
			)
			return
		}
		http.Error(w, "body read failed", http.StatusBadRequest)
		r.logger.Warn("webhooks.body_read_failed",
			"source", sourceName,
			"error", err,
		)
		return
	}

	// Verify HMAC unless the source opted out (gitlab-style plain
	// token). Opted-out sources own their auth inside Normalize — the
	// router still passes them through, but reviewers should treat
	// any non-HMAC adapter with extra scrutiny.
	if header := source.SignatureHeader(); header != "" {
		current, previous := source.Secrets()
		if err := VerifyHMACSHA256(req.Header.Get(header), body, []byte(current), []byte(previous)); err != nil {
			status := hmacStatusCode(err)
			http.Error(w, err.Error(), status)
			r.logger.Warn("webhooks.signature_failed",
				"source", sourceName,
				"reason", err.Error(),
				"remote_addr", req.RemoteAddr,
			)
			return
		}
	}

	// Replay the body for Normalize. The standard pattern: replace
	// Body with a no-op closer over a bytes.Reader on the same
	// buffer. The adapter sees a fresh Body stream identical to what
	// HMAC just hashed.
	req.Body = io.NopCloser(bytes.NewReader(body))

	event, err := source.Normalize(req)
	switch {
	case err == nil && event == nil:
		// Adapter contract violation. Treat as 500 because this is a
		// "we shipped a buggy adapter", not a caller-fixable problem.
		http.Error(w, "adapter returned nil event without error", http.StatusInternalServerError)
		r.logger.Error("webhooks.adapter_contract_violation",
			"source", sourceName,
		)
		return

	case errors.Is(err, ErrUnsupportedEvent):
		// Structurally valid event we deliberately skip — e.g.
		// workflow_run conclusion=success. 204 No Content tells the
		// caller "got it, nothing to do" so they don't retry.
		w.WriteHeader(http.StatusNoContent)
		r.logger.Debug("webhooks.unsupported_event",
			"source", sourceName,
		)
		return

	case errors.Is(err, ErrSchemaMismatch):
		// Pinned schema version doesn't match payload. Loud 400 so
		// the operator notices on the upstream Deliveries dashboard;
		// observability alert fires on rate > 0 (it should be 0 in
		// steady state).
		http.Error(w, "schema mismatch", http.StatusBadRequest)
		r.logger.Warn("webhooks.schema_mismatch",
			"source", sourceName,
			"error", err,
		)
		return

	case err != nil:
		// Anything else from the adapter is genuinely unexpected.
		// 500 + alert.
		http.Error(w, "normalize failed", http.StatusInternalServerError)
		r.logger.Error("webhooks.normalize_failed",
			"source", sourceName,
			"error", err,
		)
		return
	}

	// Success: event is fully normalized. PR3 inserts into
	// cascade_retrigger here and returns 200; PR2 ships 202 Accepted
	// because no durable record is taken yet — vendors that retry on
	// 5xx will not retry on 202, so this remains correct under
	// feature-flag-on-but-PR3-not-yet-shipped scenarios.
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = r.clock()
	}
	event.Source = sourceName

	w.WriteHeader(http.StatusAccepted)
	r.logger.Info("webhooks.received",
		"source", sourceName,
		"event_id", event.EventID.String(),
		"event_type", event.EventType,
		"pr_number", event.PRNumber,
		"head_sha", event.HeadSHA,
	)
}
