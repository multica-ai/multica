package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fleetServerOpts configures the stub Fleet server used in tests. Each
// field is optional — zero values give a default 200 success response
// with a fixed owner/instance binding.
type fleetServerOpts struct {
	statusCode int
	body       string
	delay      time.Duration
	// recordReqs is incremented on each verify call we receive, so a
	// test can assert "the cache short-circuits the HTTP layer" by
	// verifying this counter doesn't move on a cache hit.
	recordReqs *int32
	// expectToken, if non-empty, fails the test if the request body
	// does not contain this exact token plaintext.
	expectToken string
}

func newFleetServer(t *testing.T, opts fleetServerOpts) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.recordReqs != nil {
			atomic.AddInt32(opts.recordReqs, 1)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/pat/verify" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if opts.expectToken != "" {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), opts.expectToken) {
				t.Errorf("request body missing expected token; got: %s", string(body))
			}
		}
		if opts.delay > 0 {
			time.Sleep(opts.delay)
		}
		status := opts.statusCode
		if status == 0 {
			status = http.StatusOK
		}
		body := opts.body
		if body == "" {
			body = `{
				"valid": true,
				"owner_id": "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001",
				"instance_id": "i-0123456789abcdef0",
				"instance_record_id": "01972f7e-8a13-72a1-bbb0-0874ed4e8e67"
			}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

// TestCloudPATVerifier_NilSafe pins the nil-receiver contract: a server
// without MULTICA_CLOUD_FLEET_URL configured constructs nil here, and
// the middleware nil-checks before calling. Verify must still return a
// classifiable error so the middleware emits a deterministic 401 instead
// of a nil-deref panic.
func TestCloudPATVerifier_NilSafe(t *testing.T) {
	var v *CloudPATVerifier
	if v.Configured() {
		t.Fatal("nil verifier reported Configured()=true")
	}
	_, err := v.Verify(context.Background(), "mcn_anything", nil)
	if !errors.Is(err, ErrCloudPATNotConfigured) {
		t.Fatalf("expected ErrCloudPATNotConfigured, got %v", err)
	}
}

// TestCloudPATVerifier_EmptyURLReturnsNil confirms that an empty
// FleetBaseURL yields a nil verifier (not a verifier that explodes on
// first request). This is the explicit signal to the middleware that
// mcn_ is unsupported on this deployment.
func TestCloudPATVerifier_EmptyURLReturnsNil(t *testing.T) {
	if v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: "  "}); v != nil {
		t.Fatalf("expected nil for empty URL, got %#v", v)
	}
}

// TestCloudPATVerifier_VerifySuccess exercises the happy path: Fleet
// returns valid=true, the verifier surfaces owner_id / instance_id /
// instance_record_id verbatim. We don't run a Redis here, so the cache
// path is exercised separately in TestCloudPATVerifier_CacheHitSkipsHTTP.
func TestCloudPATVerifier_VerifySuccess(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{expectToken: "mcn_test_token"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	if v == nil {
		t.Fatal("verifier should not be nil")
	}
	id, err := v.Verify(context.Background(), "mcn_test_token", nil)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if id.OwnerID != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
		t.Errorf("unexpected owner_id: %q", id.OwnerID)
	}
	if id.InstanceID != "i-0123456789abcdef0" {
		t.Errorf("unexpected instance_id: %q", id.InstanceID)
	}
	if id.InstanceRecordID != "01972f7e-8a13-72a1-bbb0-0874ed4e8e67" {
		t.Errorf("unexpected instance_record_id: %q", id.InstanceRecordID)
	}
}

// TestCloudPATVerifier_VerifyEmptyToken pins an early-out: the middleware
// strips "Bearer " before calling Verify, so an empty plaintext is a
// programming error here, not a Fleet round-trip.
func TestCloudPATVerifier_VerifyEmptyToken(t *testing.T) {
	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: "http://example.invalid"})
	_, err := v.Verify(context.Background(), "", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("expected ErrCloudPATInvalid, got %v", err)
	}
}

// TestCloudPATVerifier_InvalidReasons walks every documented reason
// for a valid=false response and confirms each maps onto
// CloudPATInvalidError + matches errors.Is(ErrCloudPATInvalid). The
// reason string itself is preserved on the typed error for logging.
func TestCloudPATVerifier_InvalidReasons(t *testing.T) {
	reasons := []string{
		"format_invalid",
		"checksum_invalid",
		"token_not_found",
		"token_revoked",
		"token_expired",
		"owner_mismatch",
		"instance_mismatch",
	}
	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			body := `{"valid":false,"reason":"` + reason + `"}`
			srv := newFleetServer(t, fleetServerOpts{body: body})
			defer srv.Close()

			v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
			_, err := v.Verify(context.Background(), "mcn_x", nil)
			if !errors.Is(err, ErrCloudPATInvalid) {
				t.Fatalf("expected ErrCloudPATInvalid for reason %q, got %v", reason, err)
			}
			var typed *CloudPATInvalidError
			if !errors.As(err, &typed) {
				t.Fatalf("expected *CloudPATInvalidError, got %T", err)
			}
			if typed.Reason != reason {
				t.Errorf("expected Reason=%q, got %q", reason, typed.Reason)
			}
		})
	}
}

// TestCloudPATVerifier_FleetReturns500 — Fleet itself is broken. We
// must surface ErrCloudPATUnavailable so the middleware emits 503,
// not 401: a 401 here would tell a CLI/daemon to throw out a valid
// token because of a transient cloud outage.
func TestCloudPATVerifier_FleetReturns500(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{statusCode: http.StatusInternalServerError, body: "boom"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for 500, got %v", err)
	}
}

// TestCloudPATVerifier_FleetReturns400 — a malformed-request 400 from
// Fleet still maps onto Unavailable, not Invalid: the token isn't
// known to be bad, *we* are talking to Fleet wrong. The middleware
// emits 503 and the token is retried on the next request.
func TestCloudPATVerifier_FleetReturns400(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{statusCode: http.StatusBadRequest, body: `{"error":"bad"}`})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for 400, got %v", err)
	}
}

// TestCloudPATVerifier_NetworkError — pointing at a closed port
// simulates DNS failure / connection refused. Same Unavailable mapping.
func TestCloudPATVerifier_NetworkError(t *testing.T) {
	// Bind a server then close it immediately to get a guaranteed-
	// unreachable URL on a free port.
	srv := newFleetServer(t, fleetServerOpts{})
	url := srv.URL
	srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{
		FleetBaseURL: url,
		HTTPClient:   &http.Client{Timeout: 200 * time.Millisecond},
	})
	_, err := v.Verify(context.Background(), "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable on network error, got %v", err)
	}
}

// TestCloudPATVerifier_ValidTrueWithoutOwnerIDFailsClosed pins the
// defense for a Fleet response that says valid=true but omits
// owner_id. Without an owner_id the middleware would set X-User-ID to
// "" and trick downstream handlers into thinking the request is
// authenticated as the empty user — fail closed instead.
func TestCloudPATVerifier_ValidTrueWithoutOwnerIDFailsClosed(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{body: `{"valid":true}`})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for valid:true without owner_id, got %v", err)
	}
}

// TestCloudPATVerifier_DecodeError exercises the case where Fleet
// returns 200 but with garbage that won't decode as JSON. Treated as
// Unavailable — same logic as a 5xx.
func TestCloudPATVerifier_DecodeError(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{body: "<not json>"})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	_, err := v.Verify(context.Background(), "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable for decode error, got %v", err)
	}
}

// TestCloudPATVerifier_ContextCanceled confirms that request
// cancellation propagates as Unavailable. A canceled request is
// indistinguishable from a network failure at the auth-result level.
func TestCloudPATVerifier_ContextCanceled(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{delay: 200 * time.Millisecond})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := v.Verify(ctx, "mcn_x", nil)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable on canceled ctx, got %v", err)
	}
}

// TestCloudPATVerifier_TrimsTrailingSlash is a tiny sanity test —
// configurations sometimes carry trailing slashes; the verifier must
// normalize so it doesn't double-slash the verify path. (httptest's
// router would still accept it, but the actual Fleet won't.)
func TestCloudPATVerifier_TrimsTrailingSlash(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL + "/"})
	if v == nil {
		t.Fatal("verifier should not be nil")
	}
	if _, err := v.Verify(context.Background(), "mcn_x", nil); err != nil {
		t.Fatalf("Verify with trailing-slash baseURL failed: %v", err)
	}
}

// TestCloudPATVerifier_CacheHitSkipsHTTP confirms the Redis cache
// short-circuits the Fleet round-trip. After one successful Verify the
// next call must not increment the request counter — that's the entire
// point of the cache layer (one Fleet hit per cloudPATCacheTTL window
// per token, regardless of request rate).
func TestCloudPATVerifier_CacheHitSkipsHTTP(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &calls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	first, err := v.Verify(context.Background(), "mcn_repeat", nil)
	if err != nil {
		t.Fatalf("first Verify failed: %v", err)
	}
	if first.OwnerID == "" {
		t.Fatal("first Verify returned empty owner_id")
	}

	second, err := v.Verify(context.Background(), "mcn_repeat", nil)
	if err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if second != first {
		t.Fatalf("cache returned different identity: first=%+v second=%+v", first, second)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 fleet call, got %d", got)
	}
}

// TestCloudPATVerifier_NegativesNotCached pins the explicit choice from
// the Cloud doc: "revoke / expired / mismatch results MUST NOT be
// cached". A token that flips back to valid (lazy-revoke
// reconciliation, owner_id updated, etc.) needs to start working again
// without waiting for a TTL window.
func TestCloudPATVerifier_NegativesNotCached(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{
		body:       `{"valid":false,"reason":"token_revoked"}`,
		recordReqs: &calls,
	})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	_, err := v.Verify(context.Background(), "mcn_revoked", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("first Verify: expected invalid, got %v", err)
	}
	_, err = v.Verify(context.Background(), "mcn_revoked", nil)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("second Verify: expected invalid, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("negative result must not be cached; expected 2 fleet calls, got %d", got)
	}
}


// TestCloudPATVerifier_LookupRejectsUnknownOwner pins the new
// owner-existence guard. Cloud says the token is valid, but the
// caller's lookup says the owner_id does not exist locally — the
// verifier must reject with reason="owner_unknown" and MUST NOT
// cache the result, so a freshly-created user can authenticate
// immediately on the next call without waiting for a TTL.
func TestCloudPATVerifier_LookupRejectsUnknownOwner(t *testing.T) {
	rdb := newRedisTestClient(t)

	var calls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &calls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	lookup := func(_ context.Context, ownerID string) (bool, error) {
		// Cloud's stub returns this fixed owner_id; assert we receive
		// it before reporting "not found" so a future regression that
		// passes the wrong field would surface here.
		if ownerID != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
			t.Errorf("lookup called with unexpected owner_id: %q", ownerID)
		}
		return false, nil
	}

	first, err := v.Verify(context.Background(), "mcn_unknown_owner", lookup)
	if !errors.Is(err, ErrCloudPATInvalid) {
		t.Fatalf("expected ErrCloudPATInvalid, got %v (id=%+v)", err, first)
	}
	var typed *CloudPATInvalidError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *CloudPATInvalidError, got %T", err)
	}
	if typed.Reason != CloudPATInvalidReasonOwnerUnknown {
		t.Errorf("expected reason=%q, got %q", CloudPATInvalidReasonOwnerUnknown, typed.Reason)
	}

	// Second call: lookup now says the user exists. If the previous
	// rejection was cached, we'd still be rejected without the lookup
	// being consulted again. We must re-hit Fleet AND the lookup, and
	// succeed.
	gotLookup := false
	lookupExists := func(_ context.Context, _ string) (bool, error) {
		gotLookup = true
		return true, nil
	}
	id, err := v.Verify(context.Background(), "mcn_unknown_owner", lookupExists)
	if err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if id.OwnerID == "" {
		t.Fatal("second Verify returned empty owner_id")
	}
	if !gotLookup {
		t.Fatal("second Verify did not consult the lookup — owner_unknown was wrongly cached")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("owner_unknown must not be cached; expected 2 fleet calls, got %d", got)
	}
}

// TestCloudPATVerifier_LookupErrorMapsToUnavailable confirms that an
// infrastructure error from the lookup (DB down, query timeout, ...)
// surfaces as ErrCloudPATUnavailable so the middleware emits 503,
// not 401. Without this, a transient DB blip would tell every CLI
// and daemon to throw out a still-valid token.
func TestCloudPATVerifier_LookupErrorMapsToUnavailable(t *testing.T) {
	srv := newFleetServer(t, fleetServerOpts{})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	lookup := func(_ context.Context, _ string) (bool, error) {
		return false, errors.New("db is down")
	}
	_, err := v.Verify(context.Background(), "mcn_db_blip", lookup)
	if !errors.Is(err, ErrCloudPATUnavailable) {
		t.Fatalf("expected ErrCloudPATUnavailable, got %v", err)
	}
}

// TestCloudPATVerifier_LookupSuccessIsCached confirms that a verified
// + locally-existing owner_id IS cached: the second Verify must not
// hit Fleet OR the lookup. This is the happy-path symmetry to the
// previous two tests.
func TestCloudPATVerifier_LookupSuccessIsCached(t *testing.T) {
	rdb := newRedisTestClient(t)

	var fleetCalls int32
	srv := newFleetServer(t, fleetServerOpts{recordReqs: &fleetCalls})
	defer srv.Close()

	v := NewCloudPATVerifier(CloudPATVerifierConfig{FleetBaseURL: srv.URL, Redis: rdb})

	var lookupCalls int32
	lookup := func(_ context.Context, _ string) (bool, error) {
		atomic.AddInt32(&lookupCalls, 1)
		return true, nil
	}

	if _, err := v.Verify(context.Background(), "mcn_cacheable", lookup); err != nil {
		t.Fatalf("first Verify failed: %v", err)
	}
	if _, err := v.Verify(context.Background(), "mcn_cacheable", lookup); err != nil {
		t.Fatalf("second Verify failed: %v", err)
	}
	if got := atomic.LoadInt32(&fleetCalls); got != 1 {
		t.Fatalf("expected 1 fleet call (second hits cache), got %d", got)
	}
	if got := atomic.LoadInt32(&lookupCalls); got != 1 {
		t.Fatalf("expected 1 lookup call (second hits cache), got %d", got)
	}
}
