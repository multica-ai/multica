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

// fakeMuseAppReceptionist is a scripted stand-in for the real receptionist.
// It speaks the same wire protocol so the backend's HTTP behavior,
// polling, streaming and failure mapping are all exercised without any
// real network or agent run.
type fakeMuseAppReceptionist struct {
	mu           sync.Mutex
	statuses     []museappTaskStatus
	statusCalls  int
	events       []museappEvent
	cancelCalls  int
	executeCalls int
	executeCode  int
	statusCode   int // HTTP code for /v1/tasks polls; 0 means 200
	healthCode   int
	health       museappHealthResponse
	authHeaders  []string
	executeBody  []byte
	sinceParams  []string // ?since= values seen on /events polls
}

func (f *fakeMuseAppReceptionist) statusForCall() museappTaskStatus {
	if len(f.statuses) == 0 {
		return museappTaskStatus{Status: "running"}
	}
	i := f.statusCalls
	if i >= len(f.statuses) {
		i = len(f.statuses) - 1
	}
	return f.statuses[i]
}

func (f *fakeMuseAppReceptionist) handler() http.Handler {
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
			_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: "task-1"})
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
		_ = json.NewEncoder(w).Encode(museappEventsResponse{Events: f.events})
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
			h := f.health
			// Default to protocol v1 if the test didn't set a version.
			// (Real receptionists always return a version.)
			if h.ProtocolVersion == 0 {
				h.ProtocolVersion = 1
			}
			_ = json.NewEncoder(w).Encode(h)
		}
	})
	return mux
}

func fastMuseAppPolls(t *testing.T) {
	t.Helper()
	oldInterval := museappPollInterval.Load()
	oldBudget := museappMaxConsecutivePollErrors.Load()
	museappPollInterval.Store(int64(5 * time.Millisecond))
	museappMaxConsecutivePollErrors.Store(3)
	t.Cleanup(func() {
		museappPollInterval.Store(oldInterval)
		museappMaxConsecutivePollErrors.Store(oldBudget)
	})
}

// museappTestConfig points the backend at endpoint through the process
// environment, which is the only place the backend reads its endpoint and
// token from.
func museappTestConfig(t *testing.T, endpoint, token string) Config {
	t.Helper()
	t.Setenv(museappEnvEndpoint, endpoint)
	t.Setenv(museappEnvToken, token)
	return Config{Logger: slog.Default()}
}

func drainMuseAppSession(t *testing.T, s *Session) (Result, []Message) {
	t.Helper()
	var msgs []Message
	for m := range s.Messages {
		msgs = append(msgs, m)
	}
	res := <-s.Result
	return res, msgs
}

func TestMuseAppExecuteSuccess(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{
			{Status: "running"},
			{Status: "completed", Result: "all done"},
		},
		events: []museappEvent{
			{Seq: 1, Type: "text", Content: "hello "},
			{Seq: 2, Type: "thinking", Content: "hmm"},
			{Seq: 3, Type: "text", Content: "world"},
			{Seq: 4, Type: "tool", Tool: "read_file", Content: "reading"},
		},
		health: museappHealthResponse{ProtocolVersion: 1, Version: "9.9.9"},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, err := New("museapp", museappTestConfig(t, srv.URL, "sekret"))
	if err != nil {
		t.Fatalf("New(museapp) = %v", err)
	}
	sess, err := b.Execute(context.Background(), "do the thing", ExecOptions{Cwd: "/tmp/work-1"})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseAppSession(t, sess)

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
	var body museappExecuteRequest
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

func TestMuseAppExecuteForwardsTaskToken(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{
			{Status: "completed", Result: "done"},
		},
		health: museappHealthResponse{ProtocolVersion: 1, Version: "9.9.9"},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, err := New("museapp", museappTestConfig(t, srv.URL, "sekret"))
	if err != nil {
		t.Fatalf("New(museapp) = %v", err)
	}
	_, err = b.Execute(context.Background(), "do the thing", ExecOptions{
		Cwd:                "/tmp/work-1",
		TaskToken:          "mat_abc123",
		MulticaServerURL:   "https://multica.example.com",
		MulticaWorkspaceID: "ws-123",
	})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	var body museappExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &body); err != nil {
		t.Fatalf("decode execute body: %v", err)
	}
	if body.TaskToken != "mat_abc123" {
		t.Errorf("task_token = %q, want %q", body.TaskToken, "mat_abc123")
	}
	if body.ServerURL != "https://multica.example.com" {
		t.Errorf("server_url = %q", body.ServerURL)
	}
	if body.WorkspaceID != "ws-123" {
		t.Errorf("workspace_id = %q", body.WorkspaceID)
	}
}

func TestMuseAppExecuteEmptyPrompt(t *testing.T) {
	fake := &fakeMuseAppReceptionist{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
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

func TestMuseAppExecuteMissingEndpoint(t *testing.T) {
	t.Setenv(museappEnvEndpoint, "")
	b, err := New("museapp", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(museapp) = %v", err)
	}
	t.Setenv(museappEnvEndpoint, "")
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded without MULTICA_MUSEAPP_ENDPOINT, want error")
	} else if !strings.Contains(err.Error(), museappEnvEndpoint) {
		t.Errorf("error = %q, want it to name %s", err, museappEnvEndpoint)
	}
}

func TestMuseAppExecuteInvalidEndpoint(t *testing.T) {
	b, _ := New("museapp", museappTestConfig(t, "://not-a-url", ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded with garbage endpoint, want error")
	}
}

func TestMuseAppExecuteHTTPError(t *testing.T) {
	fake := &fakeMuseAppReceptionist{executeCode: 500}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on HTTP 500, want error")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want HTTP status", err)
	}
}

func TestMuseAppExecuteUnauthorized(t *testing.T) {
	fake := &fakeMuseAppReceptionist{executeCode: 401}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, "wrong"))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on HTTP 401, want error")
	} else if !strings.Contains(err.Error(), museappEnvToken) {
		t.Errorf("error = %q, want it to name %s", err, museappEnvToken)
	}
}

func TestMuseAppExecuteRemoteFailure(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "failed", Error: "boom"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if res.Error != "boom" {
		t.Errorf("Error = %q, want boom", res.Error)
	}
}

func TestMuseAppExecuteCancel(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{} // never completes: always "running"
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := b.Execute(ctx, "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	time.Sleep(30 * time.Millisecond) // let at least one poll round happen
	cancel()
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "aborted" {
		t.Errorf("Status = %q, want aborted", res.Status)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.cancelCalls != 1 {
		t.Errorf("cancel calls = %d, want 1", fake.cancelCalls)
	}
}

func TestMuseAppExecuteTimeout(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{} // never completes
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	sess, err := b.Execute(ctx, "hi", ExecOptions{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "timeout" {
		t.Errorf("Status = %q, want timeout", res.Status)
	}
}

func TestMuseAppExecutePollBudgetExhausted(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{statusCode: 500} // polls always fail
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "unreachable") {
		t.Errorf("Error = %q, want unreachable", res.Error)
	}
}

func TestMuseAppExecuteResumeSessionID(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "ok"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{ResumeSessionID: "muse:task-9"})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var body museappExecuteRequest
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
func TestMuseAppEndpointAndTokenIgnoreCfgEnv(t *testing.T) {
	fastMuseAppPolls(t)
	legit := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "ok"}},
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

	t.Setenv(museappEnvEndpoint, legitSrv.URL)
	t.Setenv(museappEnvToken, "daemon-token")
	b, _ := New("museapp", Config{
		Logger: slog.Default(),
		Env:    map[string]string{museappEnvEndpoint: other.URL, museappEnvToken: "agent-token"},
	})
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
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
func TestMuseAppTaskIDIsEscapedInRequestPaths(t *testing.T) {
	fastMuseAppPolls(t)
	const hostileID = "../admin/x?y=1#"
	var mu sync.Mutex
	var escaped []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		escaped = append(escaped, r.URL.EscapedPath())
		mu.Unlock()
		if r.URL.EscapedPath() == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: hostileID})
			return
		}
		_ = json.NewEncoder(w).Encode(museappTaskStatus{Status: "completed"})
	}))
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, "tok"))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	drainMuseAppSession(t, sess)

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
func TestMuseAppFinalEventsDrainedAfterTerminalStatus(t *testing.T) {
	fastMuseAppPolls(t)
	var mu sync.Mutex
	events := []museappEvent{{Seq: 1, Type: "text", Content: "head "}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: "task-1"})
	})
	mux.HandleFunc("/v1/tasks/task-1/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(museappEventsResponse{Events: events})
	})
	mux.HandleFunc("/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		// The task finishes after the backend's events poll: the tail is
		// written and the status flips in the same step.
		if len(events) == 1 {
			events = append(events, museappEvent{Seq: 2, Type: "text", Content: "tail"})
		}
		_ = json.NewEncoder(w).Encode(museappTaskStatus{Status: "completed"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseAppSession(t, sess)
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
func TestMuseAppResultIsTheAnswerNotTheEventText(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "The final answer."}},
		events: []museappEvent{
			{Seq: 1, Type: "text", Content: "Looking that up... "},
			{Seq: 2, Type: "text", Content: "Almost there."},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseAppSession(t, sess)
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
func TestMuseAppUnknownEventTypesAreIgnored(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed"}},
		events: []museappEvent{
			{Seq: 1, Type: "text", Content: "answer"},
			{Seq: 2, Type: "status", Content: "INTERNAL-STATUS"},
			{Seq: 3, Type: "", Content: "UNTYPED"},
			{Seq: 4, Type: "error", Content: "SOME-ERROR"},
		},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, msgs := drainMuseAppSession(t, sess)
	if res.Output != "answer" {
		t.Errorf("Output = %q, want only the text event", res.Output)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "INTERNAL-STATUS") || strings.Contains(m.Content, "UNTYPED") || strings.Contains(m.Content, "SOME-ERROR") {
			t.Errorf("an unknown event type reached the transcript: %+v", m)
		}
	}
}

func TestMuseAppNewWithoutEnv(t *testing.T) {
	// The SupportedTypes lockstep test constructs every backend with a bare
	// Config; museapp must not fail construction for lack of configuration —
	// that surfaces at Execute time instead.
	t.Setenv(museappEnvEndpoint, "")
	if _, err := New("museapp", Config{Logger: slog.Default()}); err != nil {
		t.Errorf("New(museapp) without env = %v, want nil", err)
	}
}

func TestProbeMuseAppReceptionist(t *testing.T) {
	ctx := context.Background()

	t.Run("healthy", func(t *testing.T) {
		fake := &fakeMuseAppReceptionist{health: museappHealthResponse{ProtocolVersion: 1, Version: "1.2.3"}}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		v, err := ProbeMuseAppReceptionist(ctx, srv.URL, "sekret")
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if v != "1.2.3" {
			t.Errorf("version = %q, want 1.2.3", v)
		}
	})

	t.Run("protocol skew", func(t *testing.T) {
		fake := &fakeMuseAppReceptionist{health: museappHealthResponse{ProtocolVersion: 999, Version: "9.9.9"}}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		if _, err := ProbeMuseAppReceptionist(ctx, srv.URL, ""); err == nil {
			t.Fatal("probe succeeded on protocol skew, want error")
		} else if !strings.Contains(err.Error(), "protocol skew") {
			t.Errorf("error = %q, want protocol skew", err)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		if _, err := ProbeMuseAppReceptionist(ctx, url, ""); err == nil {
			t.Fatal("probe succeeded on closed port, want error")
		}
	})

	t.Run("empty endpoint", func(t *testing.T) {
		if _, err := ProbeMuseAppReceptionist(ctx, "  ", ""); err == nil {
			t.Fatal("probe succeeded on empty endpoint, want error")
		}
	})

	t.Run("health 500", func(t *testing.T) {
		fake := &fakeMuseAppReceptionist{healthCode: 500}
		srv := httptest.NewServer(fake.handler())
		defer srv.Close()
		if _, err := ProbeMuseAppReceptionist(ctx, srv.URL, ""); err == nil {
			t.Fatal("probe succeeded on HTTP 500, want error")
		}
	})
}

func TestMuseAppExecuteEmptyTaskID(t *testing.T) {
	fake := &fakeMuseAppReceptionist{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: ""})
			return
		}
		fake.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	if _, err := b.Execute(context.Background(), "hi", ExecOptions{}); err == nil {
		t.Fatal("Execute succeeded on empty task id, want error")
	} else if !strings.Contains(err.Error(), "empty task id") {
		t.Errorf("error = %q, want empty task id", err)
	}
}

func TestMuseAppExecuteUnknownStatus(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "vaporware"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed (unknown status must not poll forever)", res.Status)
	}
	if !strings.Contains(res.Error, "unknown status") {
		t.Errorf("Error = %q, want unknown status", res.Error)
	}
}

// TestMuseAppCompletedWithErrorIsFailed: N1 regression.
// When the receptionist returns completed+error (e.g. "result too large"),
// the backend must report failed, not completed with transcript fallback.
func TestMuseAppCompletedWithErrorIsFailed(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{
			Status: "completed",
			Error:  "result too large (204801 bytes > 204800 byte limit)",
		}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed (completed+error is not success)", res.Status)
	}
	if !strings.Contains(res.Error, "result too large") {
		t.Errorf("Error = %q, want 'result too large'", res.Error)
	}
}

// TestMuseAppExecuteSendsProtocolVersion: Go-B3 regression.
// The execute request must carry protocol_version so the receptionist
// can reject incompatible versions before creating the task.
func TestMuseAppExecuteSendsProtocolVersion(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "done"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	drainMuseAppSession(t, sess)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.executeBody) == 0 {
		t.Fatal("no execute request captured")
	}
	var req museappExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &req); err != nil {
		t.Fatalf("failed to parse execute request: %v", err)
	}
	if req.ProtocolVersion != museappProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", req.ProtocolVersion, museappProtocolVersion)
	}
}

// TestMuseAppProtocolSkewCancelsOrphanedTask: Go-B3 regression.
// If the receptionist accepts the task but reports a version mismatch,
// the backend must attempt to cancel the orphaned task.
func TestMuseAppProtocolSkewCancelsOrphanedTask(t *testing.T) {
	fastMuseAppPolls(t)
	// Fake that returns v2 in execute response (simulating old receptionist
	// without pre-create validation)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappHealthResponse{ProtocolVersion: 1, Version: "1.0"})
	})
	var cancelCalls int
	var mu sync.Mutex
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		// Old receptionist: creates task, returns v2 in response
		_ = json.NewEncoder(w).Encode(museappExecuteResponse{
			TaskID:          "orphan-task-1",
			ProtocolVersion: 2,
		})
	})
	mux.HandleFunc("/v1/tasks/orphan-task-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cancelCalls++
		mu.Unlock()
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	_, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err == nil {
		t.Fatal("Execute should fail on protocol skew")
	}
	if !strings.Contains(err.Error(), "protocol skew") {
		t.Errorf("Error = %q, want protocol skew", err.Error())
	}
	mu.Lock()
	defer mu.Unlock()
	if cancelCalls == 0 {
		t.Error("expected cancel attempt for orphaned task, got 0")
	}
}

// TestMuseAppProbeProtocolSkew: Codex round2 item 5.
// probeMuseAppReceptionist must map version mismatch to ErrMuseAppProtocolSkew.
func TestMuseAppProbeProtocolSkew(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappHealthResponse{ProtocolVersion: 999, Version: "9.9"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := ProbeMuseAppReceptionist(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected protocol skew error")
	}
	if !strings.Contains(err.Error(), "protocol skew") {
		t.Errorf("Error = %q, want protocol skew", err.Error())
	}
}

// TestMuseAppProbeProtocolOK: matching version passes.
func TestMuseAppProbeProtocolOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappHealthResponse{ProtocolVersion: museappProtocolVersion, Version: "1.4.0"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ver, err := ProbeMuseAppReceptionist(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("Probe = %v", err)
	}
	if ver != "1.4.0" {
		t.Errorf("Version = %q, want 1.4.0", ver)
	}
}

// TestMuseAppTimeoutSRoundsUp: Codex round2 item 5.
// Sub-second timeout must round up to 1, not truncate to 0.
func TestMuseAppTimeoutSRoundsUp(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "done"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	// 500ms should round up to timeout_s=1, not 0
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	drainMuseAppSession(t, sess)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var req museappExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &req); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.TimeoutS != 1 {
		t.Errorf("TimeoutS = %d, want 1 (500ms rounds up)", req.TimeoutS)
	}
}

// TestMuseAppNoTimeoutSends24h: Codex round2 item 3.
// When Go has no deadline, send explicit 24h so the receptionist
// doesn't default to 1h and kill a task Go is happy to wait for.
func TestMuseAppNoTimeoutSends24h(t *testing.T) {
	fastMuseAppPolls(t)
	fake := &fakeMuseAppReceptionist{
		statuses: []museappTaskStatus{{Status: "completed", Result: "done"}},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	drainMuseAppSession(t, sess)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var req museappExecuteRequest
	if err := json.Unmarshal(fake.executeBody, &req); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.TimeoutS != 24*3600 {
		t.Errorf("TimeoutS = %d, want 86400 (24h default)", req.TimeoutS)
	}
}

// TestMuseAppPollFailureCancelsRemote: Codex round2 item 5.
// When polling fails exhaustively, the backend must best-effort cancel
// the remote task (B2).
func TestMuseAppPollFailureCancelsRemote(t *testing.T) {
	fastMuseAppPolls(t)
	var cancelCalls int
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappHealthResponse{ProtocolVersion: 1})
	})
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappExecuteResponse{TaskID: "t1", ProtocolVersion: 1})
	})
	// Status endpoint always fails
	mux.HandleFunc("/v1/tasks/t1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	mux.HandleFunc("/v1/tasks/t1/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(museappEventsResponse{})
	})
	mux.HandleFunc("/v1/tasks/t1/cancel", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cancelCalls++
		mu.Unlock()
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Shrink the poll budget for a fast test
	oldMax := museappMaxConsecutivePollErrors.Load()
	museappMaxConsecutivePollErrors.Store(3)
	defer museappMaxConsecutivePollErrors.Store(oldMax)

	b, _ := New("museapp", museappTestConfig(t, srv.URL, ""))
	sess, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	res, _ := drainMuseAppSession(t, sess)
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	if cancelCalls == 0 {
		t.Error("expected best-effort cancel on poll exhaustion")
	}
}
