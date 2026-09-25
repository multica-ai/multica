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
func TestWecomReplierGetsItsLanguageLookupOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	bus := events.New()
	NewRouter(nil, realtime.NewHub(), bus, analytics.NoopClient{}, nil)

	// Anti-vacuity: with no WeCom block entered, no replier is built and the
	// warning cannot appear for the reason this test names. chat:done is the
	// subscription that has been wired all along, so it marks the block ran.
	if bus.SubscriberCount(protocol.EventChatDone) == 0 {
		t.Fatal("the WeCom boot block did not run, so this test proves nothing. " +
			"Re-point this guard at wherever WeCom is wired now")
	}

	if got := logged.String(); strings.Contains(got, "no language lookup wired") {
		t.Errorf("the production replier was built without a language lookup, so every notice it sends "+
			"uses the deployment language whatever the reader's profile says — which is the whole of "+
			"what the copy pack is for. Set OutboundReplierConfig.Languages in router.go.\nlog:\n%s", got)
	}
}
