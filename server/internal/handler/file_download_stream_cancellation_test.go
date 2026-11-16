package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type cancellationStreamReader struct {
	io.Reader
	closed bool
}

type disconnectedDownloadWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
	err    error
}

func (w disconnectedDownloadWriter) Write([]byte) (int, error) {
	if w.cancel != nil {
		w.cancel()
	}
	return 0, w.err
}

func (w disconnectedDownloadWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func TestDownloadDisconnectedClientWrite(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		disconnect bool
	}{
		{"broken pipe", syscall.EPIPE, true},
		{"connection reset", fmt.Errorf("write: %w", syscall.ECONNRESET), true},
		{"closed connection", net.ErrClosed, true},
		{"content length mismatch", http.ErrContentLength, false},
		{"write deadline", context.DeadlineExceeded, false},
		{"unrelated failure", errors.New("response writer failed"), false},
	} {
		for _, ranged := range []bool{false, true} {
			for _, cancelled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/range=%v/cancelled=%v", tc.name, ranged, cancelled), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					writer := disconnectedDownloadWriter{ResponseRecorder: httptest.NewRecorder(), err: tc.err}
					if cancelled {
						writer.cancel = cancel
					}
					var logs bytes.Buffer
					previous := slog.Default()
					slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
					t.Cleanup(func() { slog.SetDefault(previous) })
					req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/download", nil)
					status := 200
					if ranged {
						req.Header.Set("Range", "bytes=0-2")
						status = 206
					}
					testHandler.serveProxyRange(writer, req, db.Attachment{SizeBytes: 3}, strings.NewReader("abc"))
					if writer.Code != status || strings.Contains(logs.String(), `"level":"ERROR"`) == (cancelled && tc.disconnect) {
						t.Fatal("client write failure changed the committed status or had the wrong log level")
					}
				})
			}
		}
	}
}

type observedDownloadWriter struct {
	http.ResponseWriter
	err error
}

func (w *observedDownloadWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.err = err
	return n, err
}

type waitForDownloadDisconnect struct {
	ctx   context.Context
	ready chan struct{}
}

func (r waitForDownloadDisconnect) Read([]byte) (int, error) {
	close(r.ready)
	select {
	case <-r.ctx.Done():
		return 0, io.EOF
	case <-time.After(3 * time.Second):
		return 0, context.DeadlineExceeded
	}
}

func TestDownloadDisconnectPreservesHTTPContentLengthError(t *testing.T) {
	type result struct {
		err, contextErr error
		logs            string
	}
	ready := make(chan struct{})
	results := make(chan result, 1)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer := &observedDownloadWriter{ResponseWriter: w}
		reader := io.MultiReader(waitForDownloadDisconnect{ctx: r.Context(), ready: ready}, strings.NewReader("oversized object"))
		testHandler.serveProxyRange(writer, r, db.Attachment{SizeBytes: 1}, reader)
		results <- result{writer.err, r.Context().Err(), logs.String()}
	}))
	t.Cleanup(server.Close)
	client, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP handler did not start reading")
	}
	client.Close()
	select {
	case got := <-results:
		if !errors.Is(got.err, http.ErrContentLength) || !errors.Is(got.contextErr, context.Canceled) {
			t.Fatalf("did not reach Content-Length failure after disconnect: %+v", got)
		}
		if !strings.Contains(got.logs, `"level":"ERROR"`) {
			t.Fatal("Content-Length failure lost its error log after disconnect")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("HTTP handler did not finish")
	}
}

func (r *cancellationStreamReader) Close() error { r.closed = true; return nil }

type cancellationStreamStorage struct {
	mockStorage
	reader *cancellationStreamReader
}

func (s *cancellationStreamStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return s.reader, nil
}

func TestDownloadStreamCancellation(t *testing.T) {
	for _, stage := range []string{"full stream", "range skip", "range stream"} {
		for _, tc := range []struct {
			name   string
			cancel bool
			err    error
		}{
			{"client cancellation", true, context.Canceled},
			{"independent cancellation", false, context.Canceled},
			{"independent deadline", false, context.DeadlineExceeded},
			{"unrelated failure after disconnect", true, errors.New("storage failure")},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				failure := &cancellationReadFailure{err: tc.err}
				if tc.cancel {
					failure.cancel = cancel
				}
				prefix := "abc"
				if stage == "range skip" {
					prefix = ""
				}
				reader := &cancellationStreamReader{Reader: io.MultiReader(strings.NewReader(prefix), failure)}
				h := *testHandler
				h.Storage = &cancellationStreamStorage{reader: reader}
				var logs bytes.Buffer
				previous := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
				t.Cleanup(func() { slog.SetDefault(previous) })
				req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/download", nil)
				status := http.StatusOK
				if stage != "full stream" {
					req.Header.Set("Range", "bytes=3-9")
					status = http.StatusPartialContent
				}
				cancelled := tc.cancel && errors.Is(tc.err, context.Canceled)
				if stage == "range skip" {
					status = http.StatusBadGateway
					if cancelled {
						status = 499
					}
				}
				response := testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
					h.proxyAttachmentDownload(w, r, db.Attachment{SizeBytes: 10, Filename: "example.txt"}, "example", false)
				}, req).Want(status)
				if !reader.closed {
					t.Fatal("stream reader was not closed")
				}
				if strings.Contains(logs.String(), `"level":"ERROR"`) == cancelled {
					t.Fatal("stream cancellation or real failure had the wrong log level")
				}
				if status == 499 && response.Body.Len() != 0 {
					t.Fatal("cancelled range skip returned a body")
				}
			})
		}
	}
}

type cancellationReadFailure struct {
	cancel context.CancelFunc
	err    error
}

func (r *cancellationReadFailure) Read([]byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
	}
	return 0, r.err
}
