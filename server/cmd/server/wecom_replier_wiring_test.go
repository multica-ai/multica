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

// The replier's language lookup is an optional field, and a missing one has no
// symptom of its own: nothing errors, nothing is empty, every notice simply
// comes out in the deployment's language whatever the reader's profile says.
// Every unit test in the wecom package sets it by hand, so the package stays
// green with the production wiring absent — which is exactly how it shipped
// absent the first time.
//
// So this asserts it off the REAL boot path: NewRouter, the same call main()
// makes. What it reads is the warning NewOutboundReplier logs when the field is
// nil, which is also what an operator would have to notice today.
//
// Two routers are built, one without the WeCom key and one with it, the same
// way wecom_bubble_wiring_test.go does: chat:done has listeners outside WeCom
// whenever another channel is configured, so only the difference between the
// two says the WeCom block ran.
func TestWecomReplierGetsItsLanguageLookupOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	t.Setenv("MULTICA_WECOM_SECRET_KEY", "")
	withoutWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withoutWecom, analytics.NoopClient{}, nil)

	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	withWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withWecom, analytics.NoopClient{}, nil)

	// Anti-vacuity: with no WeCom block entered, no replier is built and the
	// warning cannot appear for the reason this test names.
	if got, base := withWecom.SubscriberCount(protocol.EventChatDone),
		withoutWecom.SubscriberCount(protocol.EventChatDone); got <= base {
		t.Fatalf("the WeCom boot block did not run: chat:done listeners %d with the key set vs %d without, "+
			"so this test proves nothing. Re-point this guard at wherever WeCom is wired now", got, base)
	}

	if got := logged.String(); strings.Contains(got, "no language lookup wired") {
		t.Errorf("the production replier was built without a language lookup, so every notice it sends "+
			"uses the deployment language whatever the reader's profile says — which is the whole of "+
			"what the copy pack is for. Set OutboundReplierConfig.Languages in router.go.\nlog:\n%s", got)
	}
}
