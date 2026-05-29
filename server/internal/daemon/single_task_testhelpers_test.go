package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type stubAPIBehavior struct {
	taskStatus string // returned by GET /api/daemon/tasks/{id}/status
}

type stubAPIServer struct {
	*httptest.Server
	mu        sync.Mutex
	completed map[string]bool
	failed    map[string]bool
	calledFn  []string
}

func startStubAPIServer(t *testing.T, b stubAPIBehavior) *stubAPIServer {
	t.Helper()
	s := &stubAPIServer{completed: map[string]bool{}, failed: map[string]bool{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calledFn = append(s.calledFn, r.Method+" "+r.URL.Path)
		s.mu.Unlock()

		path := r.URL.Path
		switch {
		case r.Method == "POST" && strings.HasSuffix(path, "/start"):
			w.WriteHeader(http.StatusOK)
		case r.Method == "POST" && strings.HasSuffix(path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case r.Method == "POST" && strings.HasSuffix(path, "/messages"):
			w.WriteHeader(http.StatusOK)
		case r.Method == "POST" && strings.HasSuffix(path, "/usage"):
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && strings.HasSuffix(path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": b.taskStatus})
		case r.Method == "POST" && strings.HasSuffix(path, "/complete"):
			id := taskIDFromPath(path, "/complete")
			s.mu.Lock()
			s.completed[id] = true
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == "POST" && strings.HasSuffix(path, "/fail"):
			id := taskIDFromPath(path, "/fail")
			s.mu.Lock()
			s.failed[id] = true
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			_, _ = io.WriteString(w, "{}")
		}
	}))
	return s
}

func taskIDFromPath(path, suffix string) string {
	const prefix = "/api/daemon/tasks/"
	return strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
}

func (s *stubAPIServer) sawComplete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed[id]
}

func (s *stubAPIServer) sawFail(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed[id]
}

func (s *stubAPIServer) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calledFn...)
}
