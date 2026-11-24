package cli_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestDownloadFileCallerBudget(t *testing.T) {
	for _, phase := range []string{"headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			t.Setenv("MULTICA_HTTP_TIMEOUT", "100ms")
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if phase == "body" {
					_, _ = io.WriteString(w, "first-")
					w.(http.Flusher).Flush()
				}
				time.Sleep(300 * time.Millisecond)
				_, _ = io.WriteString(w, "complete")
			}))
			defer srv.Close()
			client := cli.NewAPIClient(srv.URL, "", "mat_test-token")
			defer client.HTTPClient.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			start := time.Now()
			data, err := client.DownloadFile(ctx, "/download")
			t.Logf("elapsed=%s data=%q err=%v", time.Since(start), data, err)
			want := "complete"
			if phase == "body" {
				want = "first-complete"
			}
			if err != nil || string(data) != want {
				t.Fatalf("download within caller budget: data=%q err=%v", data, err)
			}
			if client.HTTPClient.Timeout != 100*time.Millisecond || requests.Load() != 1 {
				t.Fatalf("shared timeout=%s requests=%d", client.HTTPClient.Timeout, requests.Load())
			}
		})
	}
}

func TestDownloadFileCancellationAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		timeout  time.Duration
		deadline time.Duration
		cancel   bool
	}{
		{"no deadline keeps finite client timeout", 100 * time.Millisecond, 0, false},
		{"shorter caller deadline", time.Second, 100 * time.Millisecond, false},
		{"zero client timeout with caller deadline", 0, 100 * time.Millisecond, false},
		{"cancellation during body", 100 * time.Millisecond, 3 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "partial")
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
			}))
			defer srv.Close()
			client := cli.NewAPIClient(srv.URL, "", "")
			client.HTTPClient.Timeout = tc.timeout
			defer client.HTTPClient.CloseIdleConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.deadline != 0 {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, tc.deadline)
				defer stop()
			}
			var wg sync.WaitGroup
			if tc.cancel {
				wg.Add(1)
				go func() { defer wg.Done(); <-started; cancel() }()
			}
			data, err := client.DownloadFile(ctx, "/download")
			wg.Wait()
			want := context.DeadlineExceeded
			if tc.cancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) || cli.ExitCodeFor(err) != cli.ExitNetwork {
				t.Fatalf("data=%q error=%v, want network %v", data, err, want)
			}
			if client.HTTPClient.Timeout != tc.timeout {
				t.Fatal("shared timeout changed")
			}
		})
	}
}

func TestDownloadFileAlreadyFinishedContext(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "", "")
	defer client.HTTPClient.CloseIdleConnections()
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = context.DeadlineExceeded
		} else {
			cancel()
		}
		_, err := client.DownloadFile(ctx, "/download")
		cancel()
		if !errors.Is(err, want) {
			t.Fatalf("expired=%t error=%v", expired, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("finished contexts sent %d requests", requests.Load())
	}
}

func TestDownloadFileConcurrentBudgets(t *testing.T) {
	t.Setenv("MULTICA_HTTP_TIMEOUT", "100ms")
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/long" {
			close(started)
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "complete")
	}))
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "", "")
	defer client.HTTPClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := client.DownloadFile(ctx, "/long"); result <- err }()
	<-started
	_, shortErr := client.DownloadFile(context.Background(), "/bounded")
	longErr := <-result
	if longErr != nil || !errors.Is(shortErr, context.DeadlineExceeded) {
		t.Fatalf("long=%v bounded=%v", longErr, shortErr)
	}
	if client.HTTPClient.Timeout != 100*time.Millisecond {
		t.Fatal("concurrent request changed shared timeout")
	}
}

func TestDownloadFilePreservesClientPolicies(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var redirects, requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if cookie, err := r.Cookie("download"); err != nil || cookie.Value != "cookie" {
			t.Error("client cookie jar not preserved")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/relative", http.StatusFound)
			return
		}
		if r.URL.Path == "/signed" {
			if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "sig=a%2Fb&key=x+y" {
				t.Error("signed URL auth or signature changed")
			}
		} else if r.Header.Get("Authorization") != "Bearer mat_test-token" {
			t.Error("relative URL missing task auth")
		}
		_, _ = io.WriteString(w, "complete")
	}))
	defer srv.Close()
	client := cli.NewAPIClient(srv.URL, "", "mat_test-token")
	client.HTTPClient = srv.Client()
	client.HTTPClient.Timeout = 100 * time.Millisecond
	client.HTTPClient.Jar = jar
	client.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error { redirects.Add(1); return nil }
	defer client.HTTPClient.CloseIdleConnections()
	u, _ := url.Parse(srv.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "download", Value: "cookie"}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, path := range []string{"/redirect", srv.URL + "/signed?sig=a%2Fb&key=x+y"} {
		data, err := client.DownloadFile(ctx, path)
		if err != nil || string(data) != "complete" {
			t.Fatalf("download policies: data=%q err=%v", data, err)
		}
	}
	if redirects.Load() != 1 || requests.Load() != 3 {
		t.Fatalf("redirects=%d requests=%d", redirects.Load(), requests.Load())
	}
}

func TestDownloadFileBudgetErrorClassification(t *testing.T) {
	for _, path := range []string{"/missing", "/interrupted"} {
		t.Run(path, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if path == "/missing" {
					http.Error(w, "missing", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Length", "100")
				_, _ = io.WriteString(w, "partial")
			}))
			defer srv.Close()
			client := cli.NewAPIClient(srv.URL, "", "mat_test-token")
			defer client.HTTPClient.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			data, err := client.DownloadFile(ctx, path)
			if path == "/missing" {
				var status *cli.HTTPError
				if !errors.As(err, &status) || status.StatusCode != 404 || cli.ExitCodeFor(err) != cli.ExitNotFound {
					t.Fatalf("HTTP error classification: %v", err)
				}
			} else if !errors.Is(err, io.ErrUnexpectedEOF) || cli.ExitCodeFor(err) != cli.ExitNetwork || !strings.HasPrefix(string(data), "partial") {
				t.Fatalf("body error classification: data=%q err=%v", data, err)
			}
			if requests.Load() != 1 {
				t.Fatal("download retried")
			}
		})
	}
}
