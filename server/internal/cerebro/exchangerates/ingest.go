// CEREBRO: FIR-43 service-to-service exchange-rate ingestion endpoint. The
// multica-hatchet-worker fetches USD->{targets} from Frankfurter and POSTs the
// snapshot here; the backend is the only writer of cerebro_exchange_rates, so
// the worker never needs a DATABASE_URL. Mirrors the cerebro/pricing endpoint:
// a shared service key gates it (Bearer), and when no key is configured it
// answers direct loopback callers only (local dev) and 404s everyone else so it
// is not enumerable.
package exchangerates

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// IngestRequest is the worker's POST body: the source date plus target->rate
// pairs for a single base currency.
type IngestRequest struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

type ingestResponse struct {
	Written int `json:"written"`
}

// IngestHandler upserts pushed exchange-rate snapshots into
// cerebro_exchange_rates. It owns the only DB write path for rates.
type IngestHandler struct {
	queries *cerebrodb.Queries
	token   string
}

// NewIngestHandler constructs the handler. token comes from
// CEREBRO_EXCHANGE_INGEST_KEY; an empty token switches it to loopback-only mode.
func NewIngestHandler(queries *cerebrodb.Queries, token string) *IngestHandler {
	return &IngestHandler{queries: queries, token: strings.TrimSpace(token)}
}

// Post validates the caller, decodes the snapshot, and upserts every rate.
func (h *IngestHandler) Post(w http.ResponseWriter, r *http.Request) {
	if h.token != "" {
		if !hasBearerToken(r, h.token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="exchange-rates"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else if !isDirectLoopbackRequest(r) {
		http.NotFound(w, r)
		return
	}

	var req IngestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Base = strings.TrimSpace(req.Base)
	req.Date = strings.TrimSpace(req.Date)
	if req.Base == "" || req.Date == "" || len(req.Rates) == 0 {
		writeError(w, http.StatusBadRequest, "base, date and rates are required")
		return
	}

	written, err := Store(r.Context(), h.queries, req.Base, Snapshot{Date: req.Date, Rates: req.Rates})
	if err != nil {
		slog.Error("cerebro exchange-rate ingest failed", "error", err, "base", req.Base, "date", req.Date)
		writeError(w, http.StatusInternalServerError, "failed to store exchange rates")
		return
	}

	writeJSON(w, http.StatusOK, ingestResponse{Written: written})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func hasBearerToken(r *http.Request, want string) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(auth[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isDirectLoopbackRequest(r *http.Request) bool {
	if hasForwardingHeader(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func hasForwardingHeader(r *http.Request) bool {
	for _, hdr := range []string{
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-Ip",
		"Forwarded",
	} {
		if strings.TrimSpace(r.Header.Get(hdr)) != "" {
			return true
		}
	}
	return false
}
