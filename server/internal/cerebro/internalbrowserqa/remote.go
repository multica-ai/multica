package internalbrowserqa

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type remoteVerifyRequest struct {
	App        string     `json:"app"`
	Credential Credential `json:"credential"`
}

// RemoteRunner moves browser execution to a runner colocated with the private
// Sliplane targets. The control request crosses the server boundary over TLS;
// Chromium itself only navigates to the fixed *.internal target allowlist.
type RemoteRunner struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewRemoteRunner(endpoint, token string, client *http.Client) (*RemoteRunner, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("browser verifier URL must use HTTPS or a private Sliplane internal host")
	}
	hostname := strings.ToLower(parsed.Hostname())
	privateHTTP := parsed.Scheme == "http" && (hostname == "127.0.0.1" || hostname == "localhost" || hostname == "::1" || strings.HasSuffix(hostname, ".internal"))
	if parsed.Scheme != "https" && !privateHTTP {
		return nil, fmt.Errorf("browser verifier URL must use HTTPS or a private Sliplane internal host")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("browser verifier token is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &RemoteRunner{endpoint: strings.TrimRight(parsed.String(), "/") + "/verify", token: token, client: client}, nil
}

func (r *RemoteRunner) Verify(ctx context.Context, app string, credential Credential) (Result, error) {
	if _, err := TargetFor(app); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(remoteVerifyRequest{App: app, Credential: credential})
	if err != nil {
		return Result{}, fmt.Errorf("encode browser verifier request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create browser verifier request")
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("internal browser verification failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The failure body carries the diagnostics — attempted address, stage,
		// cause and a screenshot — so read it with the same limit as a success.
		var failure struct {
			Error string `json:"error"`
			Result
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxScreenshotBytes+1<<20)).Decode(&failure)
		diagnostic := failure.Result
		if len(diagnostic.ScreenshotPNG) > maxScreenshotBytes || !bytes.HasPrefix(diagnostic.ScreenshotPNG, pngSignature) {
			diagnostic.ScreenshotPNG = nil
		}
		return diagnostic, errors.New(SafeError(errors.New(failure.Error)))
	}
	var result Result
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxScreenshotBytes+1<<20)).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("internal browser verification failed")
	}
	if result.App != strings.ToLower(strings.TrimSpace(app)) || len(result.ScreenshotPNG) > maxScreenshotBytes || !bytes.HasPrefix(result.ScreenshotPNG, pngSignature) {
		return Result{}, fmt.Errorf("internal browser verification failed")
	}
	return result, nil
}

// FailureBody is the 422 contract: the safe error message plus the diagnostic
// result. Kept in one place so the verifier runner and the API handler cannot
// drift apart on what a failed verification looks like on the wire.
func FailureBody(result Result, err error) any {
	return struct {
		Error string `json:"error"`
		Result
	}{Error: SafeError(err), Result: result}
}

type RunnerHTTPHandler struct {
	token  string
	runner *Runner
}

func NewRunnerHTTPHandler(token string, runner *Runner) http.Handler {
	return &RunnerHTTPHandler{token: token, runner: runner}
}

func (h *RunnerHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/verify" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	want := []byte("Bearer " + h.token)
	got := []byte(r.Header.Get("Authorization"))
	if len(want) != len(got) || subtle.ConstantTimeCompare(want, got) != 1 {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var request remoteVerifyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	if _, err := TargetFor(request.App); err != nil {
		http.Error(w, `{"error":"target is not allowed"}`, http.StatusBadRequest)
		return
	}
	result, err := h.runner.Verify(r.Context(), request.App, request.Credential)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(FailureBody(result, err))
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
