package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestInternalListenerConfiguration(t *testing.T) {
	s := NewService(nil, StatusCategory{})
	if server, err := NewServer("", s); err != nil || server != nil {
		t.Fatal("empty port must disable listener")
	}
	for _, port := range []string{"0", "-1", "65536", "0.0.0.0:6061", "localhost:6061", ":6061"} {
		if _, err := NewServer(port, s); err == nil {
			t.Fatalf("accepted %q", port)
		}
	}
	server, err := NewServer("6061", s)
	if err != nil || server.Addr != "127.0.0.1:6061" {
		t.Fatalf("listener=%+v err=%v", server, err)
	}
}
func TestInternalHTTPRejectsMalformedRequestsWithoutDatabase(t *testing.T) {
	h := NewHandler(NewService(nil, StatusCategory{}))
	for _, body := range []string{"null", "[]", "{} {}", "{} garbage", `{"unknown":true}`, string(bytes.Repeat([]byte("x"), 17000))} {
		t.Run(body[:min(len(body), 30)], func(t *testing.T) {
			req := httptest.NewRequest("POST", "/maintenance/jobs", bytes.NewBufferString(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/maintenance/jobs/not-a-uuid", "/maintenance/jobs/not-a-uuid/advance"} {
		method := "GET"
		if path[len(path)-7:] == "advance" {
			method = "POST"
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString("{}")))
		if w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/maintenance/jobs/"+statusID(1)+"/advance", bytes.NewBufferString("{}")))
	if w.Code != 400 {
		t.Fatal("missing revision was accepted")
	}
}
func TestInternalHTTPCreateAdvanceReplayAndConfigure(t *testing.T) {
	pool, s := fixture(t)
	seed(t, pool, "old")
	server, err := NewServer("6061", s)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	base := "http://" + listener.Addr().String()
	call := func(method, path string, body any, want int) Job {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewReader(asJSON(body)))
		if err != nil {
			t.Fatal(err)
		}
		// No application token: access is through the independent loopback listener.
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
		}
		var out struct {
			Job Job `json:"job"`
		}
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.Job
	}
	req := request(false)
	j := call("POST", "/maintenance/jobs", req, 200)
	first := call("POST", "/maintenance/jobs/"+j.ID+"/advance", map[string]any{"revision": j.Revision}, 200)
	replay := call("POST", "/maintenance/jobs/"+j.ID+"/advance", map[string]any{"revision": j.Revision}, 409)
	if replay.Revision != first.Revision {
		t.Fatal("replay advanced another batch")
	}
	paused := call("POST", "/maintenance/jobs/"+j.ID+"/pause", map[string]any{"revision": first.Revision}, 200)
	opts := Options{BatchSize: 1, DelayMS: 20, LockTimeoutMS: 10, StatementTimeoutMS: 500}
	configured := call("POST", "/maintenance/jobs/"+j.ID+"/configure", map[string]any{"revision": paused.Revision, "options": opts}, 200)
	if configured.Options != opts || configured.NextAllowedAt.Before(paused.NextAllowedAt) {
		t.Fatal("configuration was not durable")
	}
	duplicate := call("POST", "/maintenance/jobs", req, 200)
	if duplicate.ID != j.ID || duplicate.Options != opts {
		t.Fatal("creation retry after tuning did not return original job")
	}
	current := call("GET", "/maintenance/jobs/"+j.ID, nil, 200)
	if current.Revision != configured.Revision {
		t.Fatal("GET is not current")
	}
	// Build the same standalone driver shipped in the backend image.
	driver := filepath.Join(t.TempDir(), "maintenance")
	if output, err := exec.Command("go", "build", "-o", driver, "../../cmd/maintenance").CombinedOutput(); err != nil {
		t.Fatalf("build driver: %v\n%s", err, output)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	output, err := exec.Command(driver, "run", "--port", strconv.Itoa(port), "--job", j.ID, "--max-batches", "1", "--max-seconds", "15", "--resume").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("incomplete canary should exit 2: %v\n%s", err, output)
	}
	canary := call("GET", "/maintenance/jobs/"+j.ID, nil, 200)
	if canary.Status != "ready" || canary.Revision != configured.Revision+2 {
		t.Fatalf("canary did more than resume + one batch: %+v", canary)
	}
	output, err = exec.Command(driver, "run", "--port", strconv.Itoa(port), "--job", j.ID, "--max-batches", "60", "--max-seconds", "15").CombinedOutput()
	if err != nil {
		t.Fatalf("driver: %v\n%s", err, output)
	}
	completed := call("GET", "/maintenance/jobs/"+j.ID, nil, 200)
	if completed.Status != "completed" {
		t.Fatalf("driver did not finish: %s", output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
