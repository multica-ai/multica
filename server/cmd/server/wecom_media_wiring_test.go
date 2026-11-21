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

// The media resolver's queries are a constructor argument that may be nil, and
// a nil one has no symptom of its own: every attachment-failure notice simply
// comes out in the deployment's language, and is written even to an
// installation revoked since the message came in. Every unit test in the
// wecom package passes them by hand, so the package stays green with the
// production wiring absent.
//
// So this asserts it off the REAL boot path, NewRouter, by the warning
// NewMediaResolver logs when the queries are nil. The resolver is only built when
// a storage backend exists, so the test gives it one: local storage in a
// temporary directory, with S3 unset.
//
// Two routers, one without the WeCom key and one with it, as in
// wecom_replier_wiring_test.go: only the difference between them says the WeCom
// block ran.
func TestWecomMediaResolverGetsItsQueriesOnTheRealBootPath(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}
	t.Setenv("S3_BUCKET", "")
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())

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

	if got, base := withWecom.SubscriberCount(protocol.EventChatDone),
		withoutWecom.SubscriberCount(protocol.EventChatDone); got <= base {
		t.Fatalf("the WeCom boot block did not run: chat:done listeners %d with the key set vs %d without, "+
			"so this test proves nothing. Re-point this guard at wherever WeCom is wired now", got, base)
	}
	if !strings.Contains(logged.String(), "local storage initialized") {
		t.Fatalf("no storage backend came up, so the media resolver was never built and this test proves nothing.\nlog:\n%s", logged.String())
	}

	if got := logged.String(); strings.Contains(got, "wecom media: no queries wired") {
		t.Errorf("the production media resolver was built without its queries, so every "+
			"attachment-failure notice uses the deployment language whatever the reader's profile says, "+
			"and is written even after the installation is revoked. "+
			"Pass them to wecom.NewMediaResolver in router.go.\nlog:\n%s", got)
	}
}
