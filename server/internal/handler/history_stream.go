package handler

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
)

// historyBatchSize bounds rows retained by a request, independently of history length.
const historyBatchSize int32 = 32

// writeHistory streams a complete legacy array in bounded database batches.
// Small responses retain Content-Length. Large responses use HTTP streaming;
// after headers are sent a read/write failure MUST abort, never close a valid
// array around incomplete history (which clients would cache as success).
// load must close its database rows before returning, so a slow client never
// holds a database connection while this function writes the batch.
func writeHistory[T any](w http.ResponseWriter, r *http.Request, failure string, load func() ([]T, bool, error)) {
	page, more, err := load()
	if err != nil {
		slog.Warn("history read failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, failure)
		return
	}
	if !more {
		writeJSON(w, http.StatusOK, page)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Content-Length")
	w.WriteHeader(http.StatusOK)
	out := bufio.NewWriterSize(w, 32*1024)
	encoder := json.NewEncoder(out)
	abort := func(err error) {
		slog.Warn("history stream aborted", "path", r.URL.Path, "error", err)
		panic(http.ErrAbortHandler)
	}
	// A byte fits in the fresh buffer without touching the underlying writer.
	_ = out.WriteByte('[')
	first := true
	for {
		for _, item := range page {
			if err := r.Context().Err(); err != nil {
				abort(err)
			}
			if !first {
				if err := out.WriteByte(','); err != nil {
					abort(err)
				}
			}
			if err := encoder.Encode(item); err != nil {
				abort(err)
			}
			first = false
		}
		if !more {
			break
		}
		if err := out.Flush(); err != nil {
			abort(err)
		}
		// Release the previous batch before querying the next one.
		page = nil
		if err := r.Context().Err(); err != nil {
			abort(err)
		}
		page, more, err = load()
		if err != nil {
			abort(err)
		}
	}
	if _, err := out.WriteString("]\n"); err != nil {
		abort(err)
	}
	if err := out.Flush(); err != nil {
		abort(err)
	}
}
