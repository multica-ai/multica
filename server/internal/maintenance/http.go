package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/util"
)

// NewServer is a separate listener; never mount its handler on the public router.
// Empty port disables it. Only the port is configurable, never the bind address.
func NewServer(port string, service *Service) (*http.Server, error) {
	if port == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("MAINTENANCE_PORT must be 1..65535")
	}
	return &http.Server{Addr: net.JoinHostPort("127.0.0.1", port), Handler: NewHandler(service),
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 15 * time.Second}, nil
}
func NewHandler(s *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /maintenance/jobs", func(w http.ResponseWriter, r *http.Request) {
		var request CreateRequest
		if !readRequest(w, r, &request) {
			return
		}
		j, err := s.Create(r.Context(), request)
		respond(w, j, err)
	})
	mux.HandleFunc("GET /maintenance/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := util.ParseUUID(r.PathValue("id")); err != nil {
			respond(w, Job{}, ErrInvalid)
			return
		}
		j, err := s.Get(r.Context(), r.PathValue("id"))
		respond(w, j, err)
	})
	for _, action := range []string{"advance", "pause", "resume", "cancel"} {
		mux.HandleFunc("POST /maintenance/jobs/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			if _, err := util.ParseUUID(r.PathValue("id")); err != nil {
				respond(w, Job{}, ErrInvalid)
				return
			}
			var request struct {
				Revision *int64 `json:"revision"`
			}
			if !readRequest(w, r, &request) {
				return
			}
			if request.Revision == nil {
				respond(w, Job{}, ErrInvalid)
				return
			}
			j, err := s.Mutate(r.Context(), r.PathValue("id"), *request.Revision, action)
			respond(w, j, err)
		})
	}
	mux.HandleFunc("POST /maintenance/jobs/{id}/configure", func(w http.ResponseWriter, r *http.Request) {
		if _, err := util.ParseUUID(r.PathValue("id")); err != nil {
			respond(w, Job{}, ErrInvalid)
			return
		}
		var request struct {
			Revision *int64  `json:"revision"`
			Options  Options `json:"options"`
		}
		if !readRequest(w, r, &request) {
			return
		}
		if request.Revision == nil {
			respond(w, Job{}, ErrInvalid)
			return
		}
		j, err := s.Configure(r.Context(), r.PathValue("id"), *request.Revision, request.Options)
		respond(w, j, err)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func readRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var raw json.RawMessage
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&raw); err != nil {
		respond(w, Job{}, ErrInvalid)
		return false
	}
	// Decode the entire body once, rejecting trailing JSON too.
	var extra any
	if err := d.Decode(&extra); err == nil {
		respond(w, Job{}, ErrInvalid)
		return false
	} else if err != io.EOF {
		respond(w, Job{}, ErrInvalid)
		return false
	}
	if string(raw) == "null" {
		respond(w, Job{}, ErrInvalid)
		return false
	}
	if err := decode(raw, v); err != nil {
		respond(w, Job{}, err)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, j Job, err error) {
	status := http.StatusOK
	message := ""
	if err != nil {
		message = err.Error()
		switch {
		case errors.Is(err, ErrInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrBusy), errors.Is(err, ErrConflict):
			status = http.StatusConflict
		case errors.Is(err, ErrThrottled):
			status = http.StatusTooManyRequests
		default:
			status = http.StatusServiceUnavailable
		}
		slog.Warn("maintenance request failed", "job_id", j.ID, "revision", j.Revision, "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	var job *Job
	if j.ID != "" {
		job = &j
	}
	var pe *pgconn.PgError
	retryable := errors.Is(err, ErrBusy) || errors.Is(err, ErrThrottled)
	if errors.As(err, &pe) {
		retryable = pe.Code == "55P03" || pe.Code == "40P01" || pe.Code == "40001" || pe.Code == "57014"
	}
	_ = json.NewEncoder(w).Encode(struct {
		Job       *Job   `json:"job,omitempty"`
		Error     string `json:"error,omitempty"`
		Retryable bool   `json:"retryable,omitempty"`
	}{job, message, retryable})
}
