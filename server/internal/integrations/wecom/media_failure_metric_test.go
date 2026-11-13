package wecom

// media_failure_metric_test.go — every attachment that never reached the agent
// is counted once, under the reason an operator acts on.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// Per attachment, not per message: the sender hears one sentence for two
// expired links, and an operator asking how much media is being lost wants
// the two. And a guard refusal is its own reason, though the sender reads it
// as the same "did not arrive": what separates them is what the operator must
// change.
//
// REVERSE VERIFICATION: drop the recordFailure call from ResolveMedia and every
// case fails with a zero count.
func TestEveryAttachmentThatFailsIsCountedByReason(t *testing.T) {
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "209715200")
		w.WriteHeader(http.StatusOK)
	}))
	defer oversize.Close()

	image := func(url string) map[string]any {
		return map[string]any{"msgtype": "image", "image": map[string]any{"url": url, "aeskey": testAESKey}}
	}

	cases := []struct {
		name string
		// production keeps the real guard, which refuses the loopback test
		// servers; the others use the client that permits them.
		production bool
		items      []any
		want       map[string]int
	}{
		{
			name:  "two expired links are two unreadable attachments",
			items: []any{image(expired.URL), image(expired.URL)},
			want:  map[string]int{mediaDropUnreadable: 2},
		},
		{
			name:  "one too large and one expired are counted apart",
			items: []any{image(oversize.URL), image(expired.URL)},
			want:  map[string]int{mediaDropTooLarge: 1, mediaDropUnreadable: 1},
		},
		{
			name:       "an address the guard refuses is its own reason",
			production: true,
			items:      []any{image(expired.URL)},
			want:       map[string]int{mediaDropBlockedAddress: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.production {
				withAllowed(t)
			}
			storage := &fakeMediaStorage{}
			senders, _ := notifierWithLiveSocket(uuidOf(1))
			mx := newCountingMetrics()
			senders.WithMetrics(mx)
			r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders, nil, testLogger()).(*wecomMediaResolver)
			if !tc.production {
				r.http = testMediaClient()
			}

			msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": tc.items}})
			r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
			r.notices.Wait()

			for _, reason := range []string{mediaDropUnreadable, mediaDropTooLarge, mediaDropBlockedAddress} {
				if got := mx.get("media_failure:" + reason); got != tc.want[reason] {
					t.Errorf("media_failure:%s = %d, want %d", reason, got, tc.want[reason])
				}
			}
		})
	}
}

// The notice needs a live socket; the count does not. A failure with nobody to
// tell is exactly the moment it most wants recording.
func TestAFailedAttachmentIsCountedWithNoSocketToTellTheSender(t *testing.T) {
	t.Parallel()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	mx := newCountingMetrics()
	senders := newSendersRegistry().WithMetrics(mx) // no connection registered
	storage := &fakeMediaStorage{}
	r := newTestResolver(storage, newFakeMediaLedger(storage), senders)

	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": expired.URL, "aeskey": testAESKey},
	})
	r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	r.notices.Wait()

	if got := mx.get("media_failure:" + mediaDropUnreadable); got != 1 {
		t.Fatalf("media_failure:unreadable = %d, want 1", got)
	}
}

// allMediaDropReasons is the label set, read off the constants rather than
// spelled again, so a count under any of them shows up in the sums below.
var allMediaDropReasons = []string{
	mediaDropUnreadable, mediaDropTooLarge, mediaDropBlockedAddress,
	mediaDropBudgetExpired, mediaDropShutdown,
}

// The attachments the Router's budget takes are counted too, once each, under
// the budget's own reason, with no socket to tell anyone on. Two paths reach
// that: the budget is gone before ResolveMedia is called, so the resolver
// never sees the attachments at all; or it runs out mid-way, and the Router
// then drops even the refs that did land. On the second path the attachments
// split three ways — landed and dropped, cut off, failed on their own before
// the deadline — and each is counted at exactly one place, so the total is
// the number of attachments sent.
//
// REVERSE VERIFICATION: drop the recordFailure call from NotifyMediaAbandoned
// and "never started" counts nothing and "cut off" misses the landed one;
// count every in-loop failure as its own kind again and "cut off" reports
// the cut-off ones as unreadable; count len(wm.Media) on both paths and "cut
// off" counts every attachment ResolveMedia already failed on a second time.
func TestEveryAttachmentTheBudgetTookIsCountedOnce(t *testing.T) {
	landed := cosServer(t, []byte("\x89PNG\r\n\x1a\nlanded in time"), `attachment; filename="a.png"`)
	defer landed.Close()
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "209715200")
		w.WriteHeader(http.StatusOK)
	}))
	defer oversize.Close()
	slow := stallingCOS(t, 800*time.Millisecond, []byte("\x89PNG\r\n\x1a\nslower than the budget"))
	defer slow.Close()
	image := func(url string) map[string]any {
		return map[string]any{"msgtype": "image", "image": map[string]any{"url": url, "aeskey": testAESKey}}
	}

	cases := []struct {
		name     string
		items    []any
		deadline func() time.Time
		want     map[string]int
	}{
		{
			name:     "never started",
			items:    []any{image(landed.URL), image(landed.URL), image(landed.URL)},
			deadline: func() time.Time { return time.Now().Add(-time.Millisecond) },
			want:     map[string]int{mediaDropBudgetExpired: 3},
		},
		{
			// landed and then dropped by the Router, too large on its own
			// merits, cut off mid-download, and never reached before the
			// deadline.
			name:     "cut off",
			items:    []any{image(landed.URL), image(oversize.URL), image(slow.URL), image(landed.URL)},
			deadline: func() time.Time { return time.Now().Add(300 * time.Millisecond) },
			want:     map[string]int{mediaDropBudgetExpired: 3, mediaDropTooLarge: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mx := newCountingMetrics()
			senders := newSendersRegistry().WithMetrics(mx) // no connection registered
			storage := &fakeMediaStorage{}
			r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
			msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": tc.items}})

			asTheRouterDoes(t, r, tc.deadline(), mediaInstallation(), msg)

			total := 0
			for _, reason := range allMediaDropReasons {
				got := mx.get("media_failure:" + reason)
				total += got
				if got != tc.want[reason] {
					t.Errorf("media_failure:%s = %d, want %d", reason, got, tc.want[reason])
				}
			}
			if total != len(tc.items) {
				t.Errorf("%d attachments sent, %d counted: every one that never reached the agent is counted exactly once", len(tc.items), total)
			}
		})
	}
}

// A budget ended by shutdown is counted under its own reason: a deploy is
// expected to cut downloads off, and folding it into budget_expired would make
// every deploy look like a slow media host.
//
// REVERSE VERIFICATION: return mediaDropBudgetExpired for every cause in
// abandonReason and this counts three budget_expired.
func TestAttachmentsAbandonedAtShutdownAreCountedApart(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	senders := newSendersRegistry().WithMetrics(mx)
	storage := &fakeMediaStorage{}
	r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
	image := map[string]any{"msgtype": "image", "image": map[string]any{"url": "https://cos.example/x", "aeskey": testAESKey}}
	msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{image, image, image}}})

	r.NotifyMediaAbandoned(context.Background(), mediaInstallation(), msg, engine.MediaAbandonment{Cause: context.Canceled})

	if got := mx.get("media_failure:" + mediaDropShutdown); got != 3 {
		t.Errorf("media_failure:shutdown = %d, want 3", got)
	}
	if got := mx.get("media_failure:" + mediaDropBudgetExpired); got != 0 {
		t.Errorf("media_failure:budget_expired = %d, want 0", got)
	}
}
