package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The deduper is an optional field on ChannelDeps, and a missing one has no
// symptom of its own: nothing errors, and the duplicate only shows up in
// somebody's chat — one per redelivery, each spending a push from that
// conversation's quota. Every unit test in the wecom package assigns the field
// itself, so the package stays green with the production wiring absent.
//
// So this asserts it off the REAL boot path: NewRouter, the same call main()
// makes. What it reads is the warning RegisterWecom logs when the field is nil,
// which is also the only thing an operator would have to notice.
func TestWecomChannelGetsItsDeduperOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	// Anti-vacuity as a difference rather than a count: chat:done has listeners
	// that have nothing to do with WeCom, so a non-zero count proves nothing.
	withoutWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withoutWecom, analytics.NoopClient{}, nil)

	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	withWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withWecom, analytics.NoopClient{}, nil)

	if got, base := withWecom.SubscriberCount(protocol.EventChatDone),
		withoutWecom.SubscriberCount(protocol.EventChatDone); got <= base {
		t.Fatalf("the WeCom boot block did not run: chat:done listeners %d with the key set vs %d without, "+
			"so RegisterWecom was never reached and the check below proves nothing. "+
			"Re-point this guard at wherever WeCom is wired now", got, base)
	}

	if got := logged.String(); strings.Contains(got, "no deduper wired") {
		t.Errorf("the production channel was registered without a deduper, so a redelivered unreadable "+
			"message is answered again on every delivery — which is the whole of what this fixes. "+
			"Set ChannelDeps.Dedup in router.go.\nlog:\n%s", got)
	}
}
