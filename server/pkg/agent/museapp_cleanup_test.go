package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMuseAppCancellationRequiresExecutionStop(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		code             int
	}{
		{"unreachable worker", "worker still running", "cleanup_unconfirmed", 503},
		{"legacy acknowledgement", `{"ok":true}`, "cleanup_unconfirmed", 200},
		{"bookkeeping only", `{"execution_stopped":false}`, "cleanup_unconfirmed", 200},
		{"stopped execution", `{"execution_stopped":true}`, "aborted", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeMuseAppReceptionist{}
			handler := fake.handler()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/cancel") {
					w.WriteHeader(tc.code)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			backend, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "edit the shared workdir", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !session.CleanupRequired || session.TerminalObserved == nil {
				t.Fatal("daemon cannot await the cancellation outcome")
			}
			cancel()
			result, _ := drainMuseAppSession(t, session)
			if result.Status != tc.want {
				t.Fatalf("result = %+v, want %s", result, tc.want)
			}
		})
	}
}

func TestMuseAppFailureWithoutStoppedWorkerBlocksContinuation(t *testing.T) {
	for _, state := range []string{"poll error", "unknown", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			fastMuseAppPolls(t)
			fake := &fakeMuseAppReceptionist{statuses: []museappTaskStatus{{Status: state}}}
			if state == "poll error" {
				fake.statusCode = 503
			}
			srv := httptest.NewServer(fake.handler())
			defer srv.Close()
			backend, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
			session, err := backend.Execute(context.Background(), "edit the shared workdir", ExecOptions{})
			if err != nil {
				t.Fatal(err)
			}
			result, _ := drainMuseAppSession(t, session)
			if result.Status != "cleanup_unconfirmed" {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestMuseAppCancelDuringSubmissionBlocksContinuation(t *testing.T) {
	accepted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(accepted)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	backend, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-accepted; cancel() }()
	session, err := backend.Execute(ctx, "task accepted before response was lost", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, _ := drainMuseAppSession(t, session)
	if result.Status != "cleanup_unconfirmed" {
		t.Fatalf("result = %+v", result)
	}
}

func TestMuseAppProtocolSkewWithConfirmedCleanupReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: "orphan", ProtocolVersion: 2})
		} else {
			_ = json.NewEncoder(w).Encode(museappCancelResponse{ExecutionStopped: true})
		}
	}))
	defer srv.Close()
	backend, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	_, err := backend.Execute(context.Background(), "task", ExecOptions{})
	if err == nil || !strings.Contains(err.Error(), "protocol skew") {
		t.Fatalf("error = %v", err)
	}
}
