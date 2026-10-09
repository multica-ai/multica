package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMuseReceptionist is a scripted stand-in for the real receptionist.
// It speaks the same wire protocol so the backend's HTTP behavior,
// polling, streaming and failure mapping are all exercised without any
// real network or agent run.
type fakeMuseReceptionist struct {
	mu           sync.Mutex
	statuses     []museTaskStatus
	statusCalls  int
	events       []museEvent
	cancelCalls  int
	executeCalls int
	executeCode  int
	statusCode   int // HTTP code for /v1/tasks polls; 0 means 200
	healthCode   int
	health       museHealthResponse
	authHeaders  []string
	executeBody  []byte
	sinceParams  []string // ?since= values seen on /events polls
}

func (f *fakeMuseReceptionist) statusForCall() museTaskStatus {
	if len(f.statuses) == 0 {
		return museTaskStatus{Status: "running"}
	}
	i := f.statusCalls
	if i >= len(f.statuses) {
		i = len(f.statuses) - 1
	}
	return f.statuses[i]
}

func (f *fakeMuseReceptionist) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.executeCalls++
		f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
		bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		f.executeBody = bodyBytes
		code := f.executeCode
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		if code == 200 {
			_ = json.NewEncoder(w).Encode(museExecuteResponse{TaskID: "task-1"})
		}
	})
	mux.HandleFunc("/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		code := f.statusCode
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		if code == 200 {
			st := f.statusForCall()
			f.statusCalls++
			_ = json.NewEncoder(w).Encode(st)
		}
	})
	mux.HandleFunc("/v1/tasks/task-1/events", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sinceParams = append(f.sinceParams, r.URL.Query().Get("since"))
		// Deliberately ignores ?since= and always returns the full log:
		// the backend must dedupe by seq itself.
		_ = json.NewEncoder(w).Encode(museEventsResponse{Events: f.events})
	})
	mux.HandleFunc("/v1/tasks/task-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cancelCalls++
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		code := f.healthCode
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		if code == 200 {
			_ = json.NewEncoder(w).Encode(f.health)
		}
	})
	return mux
}

func fastMusePolls(t *testing.T) {
	t.Helper()
	oldInterval, oldBudget := musePollInterval, museMaxConsecutivePollErrors
	musePollInterval = 5 * time.Millisecond
	museMaxConsecutivePollErrors = 3
	t.Cleanup(func() {
		musePollInterval = oldInterval
		museMaxConsecutivePollErrors = oldBudget
	})
}

// museTestConfig points the backend at endpoint through the process
// environment, which is the only place the backend reads its endpoint and
// token from.
func museTestConfig(t *testing.T, endpoint, token string) Config {
	t.Helper()
	t.Setenv(museEnvEndpoint, endpoint)
	t.Setenv(museEnvToken, token)
	return Config{Logger: slog.Default()}
}

func drainMuseSession(t *testing.T, s *Session) (Result, []Message) {
	t.Helper()
	var msgs []Message
	for m := range s.Messages {
		msgs = append(msgs, m)
	}
	res := <-s.Result
	return res, msgs
}

func TestMuseExecuteSuccess(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{
			{Status: "running"},
			{Status: "completed", Result: "all done"},
		},
		events: []museEvent{
			{Seq: 1, Type: "text", Content: "hello "},
			{Seq: 2, Type: "thinking", Content: "hmm"},
			{Seq: 3, Type: "text", Content: "world"},
			{Seq: 4, Type: "tool", Tool: "read_file", Content: "reading"},
		},
		health: museHealthResponse{ProtocolVersion: 1, Version: "9.9.9"},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, err := New("muse", museTestConfig(t, srv.URL, "sekret"))
	if err != nil {
		t.Fatalf("New(muse) = %v", err)
	}
	sess, err := b.Execute(context.Background(), "do the thing", ExecOptions{Cwd: "/tmp/work-1"})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseSession(t, sess)

	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	// The receptionist's result is the answer; the text events were commentary.
	if res.Output != "all done" {
		t.Errorf("Output = %q, want %q", res.Output, "all done")
	}
	if res.SessionID != "muse:task-1" {
		t.Errorf("SessionID = %q, want muse:task-1", res.SessionID)
	}

	var sawText, sawThinking, sawTool, sawRunning int
	for _, m := range msgs {
		switch m.Type {
		case MessageText:
			sawText++
		case MessageThinking:
			sawThinking++
			if m.Content != "hmm" {
				t.Errorf("thinking content = %q", m.Content)
			}
		case MessageToolUse:
			sawTool++
			if m.Tool != "read_file" {
				t.Errorf("tool = %q", m.Tool)
			}
		case MessageStatus:
			sawRunning++
		}
	}
	// The fake returns the full event log on every poll; each event must be
	// emitted exactly once.
	if sawText != 2 || sawThinking != 1 || sawTool != 1 {
		t.Errorf("message counts text=%d thinking=%d tool=%d, want 2/1/1", sawText, sawThinking, sawTool)
	}
	if sawRunning != 1 {
		t.Errorf("status messages = %d, want 1", sawRunning)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.executeCalls != 1 {
		t.Errorf("execute calls = %d, want 1", fake.executeCalls)
	}
	if len(fake.authHeaders) == 0 || fake.authHeaders[0] != "Bearer sekret" {
		t.Errorf("auth headers = %v, want [Bearer sekret]", fake.authHeaders)
	}
	var body museExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &body); err != nil {
		t.Fatalf("decode execute body: %v", err)
	}
	if body.Prompt != "do the thing" {
		t.Errorf("prompt = %q", body.Prompt)
	}
	if body.WorkDir != "/tmp/work-1" {
		t.Errorf("workdir = %q, want /tmp/work-1", body.WorkDir)
	}
	// The backend must poll incrementally: first ?since=0, then ?since=4
	// once the four scripted events are consumed.
	if len(fake.sinceParams) == 0 {
		t.Error("no ?since= params observed on /events polls")
	} else {
		if fake.sinceParams[0] != "0" {
			t.Errorf("first since = %q, want 0", fake.sinceParams[0])
		}
		advanced := false
		for _, s := range fake.sinceParams[1:] {
			if s != "0" && s != "" {
				advanced = true
			}
		}
		if !advanced {
			t.Errorf("since never advanced past 0: %v", fake.sinceParams)
		}
	}
}

func TestMuseExecuteEmptyPrompt(t *testing.T) {
	fake := &fakeMuseReceptionist{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	for _, prompt := range []string{"", "   ", "\n\t "} {
		if _, err := b.Execute(context.Background(), prompt, ExecOptions{}); err == nil {
			t.Errorf("Execute(%q) succeeded, want error", prompt)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.executeCalls != 0 {
		t.Errorf("execute calls = %d, want 0 (rejected before any HTTP)", fake.executeCalls)
	}
}

func TestMuseExecuteMissingEndpoint(t *testing.T) {
	t.Setenv(museEnvEndpoint, "")
	b, err := New("muse", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(muse) = %v", err)
	}
	t.Setenv(museEnvEndpoint, "")
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded without MUSE_ENDPOINT, want error")
	} else if !strings.Contains(err.Error(), museEnvEndpoint) {
		t.Errorf("error = %q, want it to name %s", err, museEnvEndpoint)
	}
}

func TestMuseExecuteInvalidEndpoint(t *testing.T) {
	b, _ := New("muse", museTestConfig(t, "://not-a-url", ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded with garbage endpoint, want error")
	}
}

func TestMuseExecuteHTTPError(t *testing.T) {
	fake := &fakeMuseReceptionist{executeCode: 500}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on HTTP 500, want error")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want HTTP status", err)
	}
}

func TestMuseExecuteUnauthorized(t *testing.T) {
	fake := &fakeMuseReceptionist{executeCode: 401}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, "wrong"))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on HTTP 401, want error")
	} else if !strings.Contains(err.Error(), museEnvToken) {
		t.Errorf("error = %q, want it to name %s", err, museEnvToken)
	}
}

func TestMuseExecuteRemoteFailure(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "failed", Error: "boom"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if res.Error != "boom" {
		t.Errorf("Error = %q, want boom", res.Error)
	}
}

func TestMuseExecuteCancel(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{} // never completes: always "running"
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := b.Execute(ctx, "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	time.Sleep(30 * time.Millisecond) // let at least one poll round happen
	cancel()
	res, _ := drainMuseSession(t, sess)
	if res.Status != "aborted" {
		t.Errorf("Status = %q, want aborted", res.Status)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.cancelCalls != 1 {
		t.Errorf("cancel calls = %d, want 1", fake.cancelCalls)
	}
}

func TestMuseExecuteTimeout(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{} // never completes
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	sess, err := b.Execute(ctx, "hi", ExecOptions{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "timeout" {
		t.Errorf("Status = %q, want timeout", res.Status)
	}
}

func TestMuseExecutePollBudgetExhausted(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{statusCode: 500} // polls always fail
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "unreachable") {
		t.Errorf("Error = %q, want unreachable", res.Error)
	}
}

func TestMuseExecuteResumeSessionID(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "completed", Result: "ok"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{ResumeSessionID: "muse:task-9"})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var body museExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &body); err != nil {
		t.Fatalf("decode execute body: %v", err)
	}
	if body.SessionID != "muse:task-9" {
		t.Errorf("session_id = %q, want muse:task-9", body.SessionID)
	}
}

// The agent's custom_env reaches the backend as cfg.Env. It must not be able to
// redirect the backend: the receptionist is whatever the daemon's own
// environment says, and the daemon's token must never be sent anywhere else.
func TestMuseEndpointAndTokenIgnoreCfgEnv(t *testing.T) {
	fastMusePolls(t)
	legit := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "completed", Result: "ok"}},
	}
	legitSrv := httptest.NewServer(legit.handler())
	defer legitSrv.Close()

	var mu sync.Mutex
	var elsewhere []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		elsewhere = append(elsewhere, r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer other.Close()

	t.Setenv(museEnvEndpoint, legitSrv.URL)
	t.Setenv(museEnvToken, "daemon-token")
	b, _ := New("muse", Config{
		Logger: slog.Default(),
		Env:    map[string]string{museEnvEndpoint: other.URL, museEnvToken: "agent-token"},
	})
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed (the process-env receptionist must serve the run)", res.Status)
	}
	mu.Lock()
	if len(elsewhere) != 0 {
		t.Errorf("a cfg.Env endpoint received requests: %q", elsewhere)
	}
	mu.Unlock()
	legit.mu.Lock()
	defer legit.mu.Unlock()
	if len(legit.authHeaders) == 0 || legit.authHeaders[0] != "Bearer daemon-token" {
		t.Errorf("auth headers = %v, want [Bearer daemon-token]", legit.authHeaders)
	}
}

// The task id is the receptionist's input. A hostile or buggy id must stay one
// path segment of a task URL.
func TestMuseTaskIDIsEscapedInRequestPaths(t *testing.T) {
	fastMusePolls(t)
	const hostileID = "../admin/x?y=1#"
	var mu sync.Mutex
	var escaped []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		escaped = append(escaped, r.URL.EscapedPath())
		mu.Unlock()
		if r.URL.EscapedPath() == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(museExecuteResponse{TaskID: hostileID})
			return
		}
		_ = json.NewEncoder(w).Encode(museTaskStatus{Status: "completed"})
	}))
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, "tok"))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	drainMuseSession(t, sess)

	mu.Lock()
	defer mu.Unlock()
	taskPath := "/v1/tasks/" + url.PathEscape(hostileID)
	sawTaskPath := false
	for _, p := range escaped[1:] { // [0] is /v1/execute
		if p != taskPath && p != taskPath+"/events" {
			t.Errorf("request path %q left the task URL; want %q or %q/events", p, taskPath, taskPath)
		}
		if p == taskPath {
			sawTaskPath = true
		}
	}
	if !sawTaskPath {
		t.Errorf("no status poll reached %q; paths = %q", taskPath, escaped)
	}
}

// The events and status polls are separate requests, so the receptionist can
// write its last event between them. The answer must still contain it.
func TestMuseFinalEventsDrainedAfterTerminalStatus(t *testing.T) {
	fastMusePolls(t)
	var mu sync.Mutex
	events := []museEvent{{Seq: 1, Type: "text", Content: "head "}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museExecuteResponse{TaskID: "task-1"})
	})
	mux.HandleFunc("/v1/tasks/task-1/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(museEventsResponse{Events: events})
	})
	mux.HandleFunc("/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		// The task finishes after the backend's events poll: the tail is
		// written and the status flips in the same step.
		if len(events) == 1 {
			events = append(events, museEvent{Seq: 2, Type: "text", Content: "tail"})
		}
		_ = json.NewEncoder(w).Encode(museTaskStatus{Status: "completed"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseSession(t, sess)
	if res.Output != "head tail" {
		t.Errorf("Output = %q, want %q", res.Output, "head tail")
	}
	var texts []string
	for _, m := range msgs {
		if m.Type == MessageText {
			texts = append(texts, m.Content)
		}
	}
	if strings.Join(texts, "") != "head tail" {
		t.Errorf("streamed text = %q, want %q", texts, "head tail")
	}
}

// "result" is the receptionist's final answer; the text events are commentary
// on the way there. The answer must be the result, and the commentary must
// still reach the transcript.
func TestMuseResultIsTheAnswerNotTheEventText(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "completed", Result: "The final answer."}},
		events: []museEvent{
			{Seq: 1, Type: "text", Content: "Looking that up... "},
			{Seq: 2, Type: "text", Content: "Almost there."},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseSession(t, sess)
	if res.Output != "The final answer." {
		t.Errorf("Output = %q, want the receptionist's result", res.Output)
	}
	var streamed []string
	for _, m := range msgs {
		if m.Type == MessageText {
			streamed = append(streamed, m.Content)
		}
	}
	if strings.Join(streamed, "") != "Looking that up... Almost there." {
		t.Errorf("streamed text = %q, want the commentary events", streamed)
	}
}

// The protocol has three event types. Anything else is not words for the user.
func TestMuseUnknownEventTypesAreIgnored(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "completed"}},
		events: []museEvent{
			{Seq: 1, Type: "text", Content: "answer"},
			{Seq: 2, Type: "status", Content: "INTERNAL-STATUS"},
			{Seq: 3, Type: "", Content: "UNTYPED"},
			{Seq: 4, Type: "error", Content: "SOME-ERROR"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseSession(t, sess)
	if res.Output != "answer" {
		t.Errorf("Output = %q, want only the text event", res.Output)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "INTERNAL-STATUS") || strings.Contains(m.Content, "UNTYPED") || strings.Contains(m.Content, "SOME-ERROR") {
			t.Errorf("an unknown event type reached the transcript: %+v", m)
		}
	}
}

func TestMuseNewWithoutEnv(t *testing.T) {
	// The SupportedTypes lockstep test constructs every backend with a bare
	// Config; muse must not fail construction for lack of configuration —
	// that surfaces at Execute time instead.
	t.Setenv(museEnvEndpoint, "")
	if _, err := New("muse", Config{Logger: slog.Default()}); err != nil {
		t.Errorf("New(muse) without env = %v, want nil", err)
	}
}

func TestProbeMuseReceptionist(t *testing.T) {
	ctx := context.Background()

	t.Run("healthy", func(t *testing.T) {
		fake := &fakeMuseReceptionist{health: museHealthResponse{ProtocolVersion: 1, Version: "1.2.3"}}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		v, err := ProbeMuseReceptionist(ctx, srv.URL, "sekret")
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if v != "1.2.3" {
			t.Errorf("version = %q, want 1.2.3", v)
		}
	})

	t.Run("protocol skew", func(t *testing.T) {
		fake := &fakeMuseReceptionist{health: museHealthResponse{ProtocolVersion: 999, Version: "9.9.9"}}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		if _, err := ProbeMuseReceptionist(ctx, srv.URL, ""); err == nil {
			t.Fatal("probe succeeded on protocol skew, want error")
		} else if !strings.Contains(err.Error(), "protocol skew") {
			t.Errorf("error = %q, want protocol skew", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		if _, err := ProbeMuseReceptionist(ctx, url, ""); err == nil {
			t.Fatal("probe succeeded on closed port, want error")
		}
	})

	t.Run("empty endpoint", func(t *testing.T) {
		if _, err := ProbeMuseReceptionist(ctx, "  ", ""); err == nil {
			t.Fatal("probe succeeded on empty endpoint, want error")
		}
	})

	t.Run("health 500", func(t *testing.T) {
		fake := &fakeMuseReceptionist{healthCode: 500}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		if _, err := ProbeMuseReceptionist(ctx, srv.URL, ""); err == nil {
			t.Fatal("probe succeeded on HTTP 500, want error")
		}
	})
}

func TestMuseExecuteEmptyTaskID(t *testing.T) {
	fake := &fakeMuseReceptionist{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(museExecuteResponse{TaskID: ""})
			return
		}
		fake.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on empty task id, want error")
	} else if !strings.Contains(err.Error(), "empty task id") {
		t.Errorf("error = %q, want empty task id", err)
	}
}

func TestMuseExecuteUnknownStatus(t *testing.T) {
	fastMusePolls(t)
	fake := &fakeMuseReceptionist{
		statuses: []museTaskStatus{{Status: "vaporware"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("muse", museTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed (unknown status must not poll forever)", res.Status)
	}
	if !strings.Contains(res.Error, "unknown status") {
		t.Errorf("Error = %q, want unknown status", res.Error)
	}
}
