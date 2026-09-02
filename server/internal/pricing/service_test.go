package pricing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type pricingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f pricingRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFetchFeedRetainsSafeFailureDetails(t *testing.T) {
	client := &http.Client{Transport: pricingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	_, err := fetchFeed(context.Background(), client, "https://private-user:private-password@feed.invalid/prices?token=private-token", feedState{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request failure lost its timeout cause: %v", err)
	}
	for _, secret := range []string{"private-user", "private-password", "private-token", "token="} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("request diagnostic exposed URL credentials: %v", err)
		}
	}

	client.Transport = pricingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}, nil
	})
	_, err = fetchFeed(context.Background(), client, "https://feed.invalid/prices", feedState{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("body failure lost its cause: %v", err)
	}
}

func TestRefreshDueDistinguishesFailureFromDailySuccess(t *testing.T) {
	svc := New(nil, nil)
	svc.Location = time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 9, 2, 16, 30, 0, 0, time.UTC)
	stamp := func(at time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: at, Valid: true} }
	for _, tc := range []struct {
		name     string
		previous db.ModelPricingCatalog
		force    bool
		wantDue  bool
		wantErr  bool
	}{
		{name: "initial sync", wantDue: true},
		{name: "retry initial failure", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-2 * time.Minute)), LastError: "failed"}, wantDue: true},
		{name: "retry failure after success", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-2 * time.Minute)), SucceededAt: stamp(now.Add(-3 * time.Minute)), LastError: "failed"}, wantDue: true},
		{name: "failure cooldown remains failed", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-30 * time.Second)), LastError: "failed"}, wantErr: true},
		{name: "forced failure cooldown remains failed", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-30 * time.Second)), LastError: "failed"}, force: true, wantErr: true},
		{name: "daily success", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-2 * time.Minute)), SucceededAt: stamp(now.Add(-2 * time.Minute))}},
		{name: "forced same-day refresh", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-2 * time.Minute)), SucceededAt: stamp(now.Add(-2 * time.Minute))}, force: true, wantDue: true},
		{name: "success cooldown", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-30 * time.Second)), SucceededAt: stamp(now.Add(-30 * time.Second))}, force: true},
		{name: "previous local day", previous: db.ModelPricingCatalog{CheckedAt: stamp(now.Add(-time.Hour)), SucceededAt: stamp(now.Add(-time.Hour))}, wantDue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			due, err := svc.refreshDue(tc.previous, now, tc.force)
			if due != tc.wantDue || errors.Is(err, ErrRefresh) != tc.wantErr {
				t.Fatalf("due=%v err=%v, want due=%v refresh error=%v", due, err, tc.wantDue, tc.wantErr)
			}
		})
	}
}

func TestFetchFeedConditionalAndFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(`{"models":{}}`))
	}))
	defer server.Close()
	first, err := fetchFeed(context.Background(), server.Client(), server.URL, feedState{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetchFeed(context.Background(), server.Client(), server.URL, first)
	if err != nil || string(first.Body) != string(second.Body) || calls != 2 {
		t.Fatalf("304 lost snapshot: %+v %v", second, err)
	}
	for _, code := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusOK} {
		broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code); w.Write([]byte("not JSON")) }))
		_, err := fetchFeed(context.Background(), broken.Client(), broken.URL, first)
		broken.Close()
		if err == nil {
			t.Fatalf("accepted broken response %d", code)
		}
	}
}

func TestMidnightUsesDeploymentCalendar(t *testing.T) {
	for _, test := range []struct{ zone, now, want string }{
		{"Asia/Shanghai", "2026-09-01T16:00:00Z", "2026-09-01T16:00:00Z"},
		{"Asia/Shanghai", "2026-09-01T15:59:59Z", "2026-08-31T16:00:00Z"},
		{"America/New_York", "2026-03-08T12:00:00Z", "2026-03-08T05:00:00Z"},
		{"America/New_York", "2026-03-09T12:00:00Z", "2026-03-09T04:00:00Z"},
	} {
		loc, err := time.LoadLocation(test.zone)
		if err != nil {
			t.Fatal(err)
		}
		now, _ := time.Parse(time.RFC3339, test.now)
		if got := Midnight(now, loc).Format(time.RFC3339); got != test.want {
			t.Fatalf("%s: %s != %s", test.zone, got, test.want)
		}
	}
}

// Optional smoke test reads downloaded public feeds, never calls a network or agent.
func TestDownloadedCatalog(t *testing.T) {
	path := os.Getenv("MODEL_PRICING_TEST_LITELLM_FILE")
	if path == "" {
		t.Skip("no downloaded feed specified")
	}
	lite, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	models, err := os.ReadFile(os.Getenv("MODEL_PRICING_TEST_MODELS_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := BuildCatalog(lite, models)
	if err != nil {
		t.Fatal(err)
	}
	subscription, ok := Resolve(c, nil, "custom:k3-256k", "hermes")
	api, _ := Resolve(c, nil, "kimi-k3", "moonshotai")
	if !ok || subscription.Input <= 0 || subscription.Input != api.Input || subscription.Output != api.Output {
		t.Fatalf("subscription %+v != API %+v", subscription, api)
	}
	t.Logf("validated %d rows; Kimi subscription uses %s/%s", len(c.Rows), subscription.Provider, subscription.Model)
}
