// Package attachmentserve is the cerebro-fork helper that streams a proxied
// attachment back to the browser with HTTP Range support.
//
// Why this exists (FIR-1673): the upstream proxy download wrote the whole
// object with a single io.Copy and Cache-Control: no-store. On a flaky / mobile
// connection the transfer could stall mid-stream, leaving the browser with a
// half-loaded image and no way to resume — the "image stops mid-load" bug. By
// routing media through http.ServeContent we get conditional requests and HTTP
// Range / 206 Partial Content for free, so a client can request the bytes it is
// missing instead of giving up on the whole response.
//
// The bytes are buffered into a bytes.Reader (an io.ReadSeeker, which
// ServeContent requires) only for objects up to maxBufferBytes — images and the
// other previewable types are always small. Larger objects keep the original
// streaming path so a big video never has to fit in memory.
package attachmentserve

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxBufferBytes caps how large an object we are willing to read fully into
// memory to gain Range support. Previewable attachments (images, PDFs, text)
// sit far below this; anything larger falls back to plain streaming.
const maxBufferBytes = 25 << 20 // 25 MB

// ServeRangeable writes the attachment body to w. For objects within the buffer
// cap it serves via http.ServeContent (Range / conditional support); larger or
// unknown-size objects stream straight through with io.Copy.
//
// contentType / disposition are the already-resolved header values from the
// caller (including the cerebro force-download disposition). modTime is used by
// http.ServeContent for Last-Modified / If-Modified-Since; pass the zero time to
// omit it.
func ServeRangeable(
	w http.ResponseWriter,
	r *http.Request,
	contentType, disposition string,
	sizeBytes int64,
	modTime time.Time,
	reader io.Reader,
) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Small enough to buffer: serve with Range support so a stalled/partial
	// load can resume instead of failing the whole image.
	if sizeBytes >= 0 && sizeBytes <= maxBufferBytes {
		data, err := io.ReadAll(io.LimitReader(reader, maxBufferBytes+1))
		if err != nil {
			slog.Error("attachmentserve: failed to buffer attachment", "error", err)
			http.Error(w, "failed to read attachment", http.StatusBadGateway)
			return
		}
		if int64(len(data)) <= maxBufferBytes {
			// no-store keeps the upstream policy: never persist the bytes to a
			// shared cache. Range still works within the response's lifetime,
			// which is what lets a stalled image load recover.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Accept-Ranges", "bytes")
			http.ServeContent(w, r, "", modTime, bytes.NewReader(data))
			return
		}
		// Size metadata lied (object grew past the cap) — fall back to streaming
		// the bytes we already read plus whatever remains.
		reader = io.MultiReader(bytes.NewReader(data), reader)
	}

	// Streaming fallback for large / unknown-size objects.
	w.Header().Set("Cache-Control", "no-store")
	if sizeBytes >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sizeBytes))
	}
	if _, err := io.Copy(w, reader); err != nil {
		slog.Error("attachmentserve: failed to stream attachment", "error", err)
	}
}
