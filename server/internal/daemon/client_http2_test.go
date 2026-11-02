package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func newHTTP2TestClient(t *testing.T, handler http.Handler) (*Client, <-chan *testutil.BlackholeConn) {
	t.Helper()
	server, connections := testutil.NewBlackholeHTTP2Server(t, handler)
	client := NewClient(server.URL)
	for _, httpClient := range []*http.Client{client.client, client.bundleClient} {
		testutil.TrustHTTP2Server(t, httpClient, server)
	}
	client.client.Timeout = 2 * time.Second
	return client, connections
}

func TestClientHTTP2RecoversBlackholedConnection(t *testing.T) {
	started := make(chan struct{}, 1)
	var posts atomic.Int32
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := client.getJSON(ctx, "/warm", nil); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	result := make(chan error, 1)
	go func() { result <- client.ReportProgress(ctx, "task", "running", 1, 2) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("POST did not reach server")
	}
	conn.Drop.Store(true)
	if err := <-result; err == nil {
		t.Fatal("interrupted POST unexpectedly succeeded")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active stream waited for request timeout instead of connection recovery: %v", err)
	}
	if err := client.getJSON(ctx, "/recovered", nil); err != nil {
		t.Fatalf("client reused the dead connection: %v", err)
	}
	select {
	case <-connections:
	default:
		t.Fatal("recovery did not open a new connection")
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("interrupted POST was replayed: received %d calls", got)
	}
}

func TestClientHTTP2KeepsHealthySlowConnection(t *testing.T) {
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	for _, path := range []string{"/warm", "/slow", "/after"} {
		if err := client.getJSON(context.Background(), path, nil); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	<-connections
	select {
	case <-connections:
		t.Fatal("healthy connection was replaced while waiting for a response")
	default:
	}
}

func TestClientHTTP1WithConnectionHealthChecks(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("expected HTTP/1, got %s", r.Proto)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	client := NewClient(server.URL)
	defer client.CloseIdleConnections()
	client.client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if err := client.getJSON(context.Background(), "/", nil); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSkillBundleHTTP2RecoversBlackholedConnection(t *testing.T) {
	started := make(chan struct{}, 1)
	var requests atomic.Int32
	const response = `{"bundles":[{"id":"skill","content":"skill instructions"}]}`
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if requests.Add(1) == 2 {
			_, _ = io.WriteString(w, `{"bundles":[`)
			w.(http.Flusher).Flush()
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, response)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	ref := SkillRefData{ID: "skill"}
	if _, _, err := client.ResolveSkillBundle(ctx, "runtime", "task", ref); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	type result struct {
		bundle SkillData
		stats  TransferStats
		err    error
	}
	done := make(chan result, 1)
	go func() {
		bundle, stats, err := client.ResolveSkillBundle(ctx, "runtime", "task", ref)
		done <- result{bundle, stats, err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("bundle request did not reach server")
	}
	conn.Drop.Store(true)
	got := <-done
	if got.err != nil {
		t.Fatalf("bundle did not recover within its context deadline: %v", got.err)
	}
	if got.bundle.ID != ref.ID || got.bundle.Content != "skill instructions" {
		t.Fatalf("unexpected recovered bundle: %+v", got.bundle)
	}
	if !got.stats.ResponseStarted || got.stats.BytesRead != int64(len(response)) {
		t.Fatalf("unexpected transfer stats after retry: %+v", got.stats)
	}
	if count := requests.Load(); count != 3 {
		t.Fatalf("expected warmup, interrupted download and one retry; got %d requests", count)
	}
	select {
	case <-connections:
	default:
		t.Fatal("bundle recovery did not open a new connection")
	}
}

func TestResolveSkillBundleHTTP2SlowDownloadUsesContextDeadline(t *testing.T) {
	for _, budget := range []time.Duration{3 * time.Second, 100 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			var requests atomic.Int32
			client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, `{"bundles":[`)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(500 * time.Millisecond):
					_, _ = io.WriteString(w, `{"id":"skill"}]}`)
				case <-r.Context().Done():
				}
			}))
			client.client.Timeout = 50 * time.Millisecond
			if client.bundleClient.Timeout != 0 {
				t.Fatal("bundle downloads must use the caller's deadline")
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			bundle, stats, err := client.ResolveSkillBundle(ctx, "runtime", "task", SkillRefData{ID: "skill"})
			if budget > 500*time.Millisecond {
				if err != nil || bundle.ID != "skill" {
					t.Fatalf("healthy slow download failed: bundle=%+v err=%v", bundle, err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected caller deadline, got %v", err)
			}
			if !stats.ResponseStarted || stats.BytesRead == 0 {
				t.Fatalf("missing body progress: %+v", stats)
			}
			if count := requests.Load(); count != 1 {
				t.Fatalf("unexpected retry during slow download or after deadline: %d", count)
			}
			<-connections
			select {
			case <-connections:
				t.Fatal("healthy connection was replaced during slow download")
			default:
			}
		})
	}
}

func TestClientCloseIdleConnectionsClosesBothPools(t *testing.T) {
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"bundles":[{"id":"skill"}]}`)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for pass := 0; pass < 2; pass++ {
		if err := client.getJSON(ctx, "/warm", nil); err != nil {
			t.Fatal(err)
		}
		if _, _, err := client.ResolveSkillBundle(ctx, "runtime", "task", SkillRefData{ID: "skill"}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			select {
			case <-connections:
			default:
				t.Fatalf("pass %d: both pools must open their own connection", pass)
			}
		}
		client.CloseIdleConnections()
	}
}
