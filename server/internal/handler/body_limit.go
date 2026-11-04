package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Size caps for the governance and office JSON bodies (spec 4.9). The product
// has no global body limit, so each of these handlers caps its own body before
// decoding it: a request over the cap is answered 413 and never read into memory.
const (
	// governanceJSONBodyLimit bounds the small JSON bodies of the metric, goal,
	// budget and standing approval writes. The largest legitimate one, a goal or
	// a policy with its match, is a few kilobytes.
	governanceJSONBodyLimit int64 = 16 << 10
	// companyImportBodyLimit bounds a company import document. An export carries
	// every agent's instructions and every squad's charter and routines.
	companyImportBodyLimit int64 = 8 << 20
)

// readCappedJSON decodes a request body of at most limit bytes into dst. It
// answers 413 for a body over the cap, and 400 with badMsg for any other decode
// failure. It reports whether the handler may continue.
func readCappedJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any, badMsg string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil {
		return true
	}
	if isBodyTooLarge(err) {
		writeBodyTooLarge(w, limit)
		return false
	}
	writeError(w, http.StatusBadRequest, badMsg)
	return false
}

// isBodyTooLarge reports whether err is a read past a MaxBytesReader cap.
func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// writeBodyTooLarge answers 413 and names the cap.
func writeBodyTooLarge(w http.ResponseWriter, limit int64) {
	writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the request body is larger than %d bytes", limit))
}
