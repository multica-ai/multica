package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// A cancelled operation is a client cancellation only when its parent request
// is cancelled too. Independent cancellation, deadlines and unrelated failures
// keep their existing error handling.
func isRequestCancellation(r *http.Request, err error) bool {
	return errors.Is(r.Context().Err(), context.Canceled) && errors.Is(err, context.Canceled)
}

func writeRequestCancellation(w http.ResponseWriter, r *http.Request, err error) bool {
	if !isRequestCancellation(r, err) {
		return false
	}
	w.WriteHeader(middleware.StatusClientClosedRequest)
	return true
}

// SQLSTATE 57014 also describes a database statement timeout. The parent
// request must be cancelled before treating it as an abandoned query.
// Successful reads may pass r.Context().Err() before writing their response.
func writeQueryCancellation(w http.ResponseWriter, r *http.Request, err error) bool {
	if writeRequestCancellation(w, r, err) {
		return true
	}
	if !errors.Is(r.Context().Err(), context.Canceled) || !isSearchStatementTimeout(err) {
		return false
	}
	w.WriteHeader(middleware.StatusClientClosedRequest)
	return true
}
