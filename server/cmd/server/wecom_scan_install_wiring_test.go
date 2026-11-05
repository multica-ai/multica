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
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// The scan-code install has three pieces boot has to assemble, and each one
// fails quietly when it is missing. Without the service the endpoints 503 and
// the settings page hides the option; without the worker's event bus a scanned
// bot does not appear on the open page; without its supervisor the bot connects
// a sweep late — and a nil *Supervisor passed through the Notifier interface
// does worse, panicking on the first scanned bot. The unit tests in the wecom
// package all build these by hand, so they stay green whatever router.go does.
//
// So this asserts it off the REAL boot path: NewRouterWithOptions, the call
// main() makes. Two routers are built, one without the WeCom key and one with,
// and the difference between them is what the WeCom block did.

func bootWecomScanInstall(t *testing.T, withKey bool) (*handler.Handler, string) {
	t.Helper()
	var logged bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(restore)

	if withKey {
		key := make([]byte, secretbox.KeySize)
		if _, err := rand.Read(key); err != nil {
			t.Fatalf("generate a wecom secretbox key: %v", err)
		}
		t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	} else {
		t.Setenv("MULTICA_WECOM_SECRET_KEY", "")
	}
	_, h := NewRouterWithOptions(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	return h, logged.String()
}

func TestWecomScanInstallIsWiredOnTheRealBootPath(t *testing.T) {
	t.Setenv("CHANNEL_WS_LEASE_BACKEND", "")

	without, _ := bootWecomScanInstall(t, false)
	if without.WecomInstall != nil || without.WecomInstallWorker != nil {
		t.Fatal("scan install is wired with no WeCom key set; this test can no longer tell the WeCom block apart from the rest of boot")
	}

	h, log := bootWecomScanInstall(t, true)
	if h.ChannelSupervisor == nil {
		t.Fatalf("the channel supervisor is off on a default boot, so the check below proves nothing; log:\n%s", log)
	}
	if h.WecomInstall == nil || h.WecomInstallWorker == nil {
		t.Fatalf("WeCom is on but scan install is not wired: service=%v worker=%v", h.WecomInstall != nil, h.WecomInstallWorker != nil)
	}
	if !h.WecomInstall.Configured() {
		t.Errorf("scan install is wired but not configured, so begin 503s and the settings page hides it; log:\n%s", log)
	}
	for _, warning := range []string{"no event bus wired", "no channel supervisor wired"} {
		if strings.Contains(log, warning) {
			t.Errorf("the install worker was built without a collaborator boot has (%q); log:\n%s", warning, log)
		}
	}
}

// When the supervisor is off (here: a Redis lease backend with no Redis), the
// worker has to receive a real nil, which it reports, rather than a nil
// *Supervisor inside a non-nil interface, which it would call.
func TestWecomScanInstallGetsANilSupervisorAsNil(t *testing.T) {
	t.Setenv("CHANNEL_WS_LEASE_BACKEND", "redis")

	h, log := bootWecomScanInstall(t, true)
	if h.ChannelSupervisor != nil {
		t.Fatalf("the supervisor came up with a Redis backend and no Redis, so this test proves nothing; log:\n%s", log)
	}
	if h.WecomInstallWorker == nil {
		t.Fatal("scan install worker not wired")
	}
	if !strings.Contains(log, "no channel supervisor wired") {
		t.Errorf("the install worker did not see the missing supervisor as missing, so the first scanned bot "+
			"calls Notify on a nil *Supervisor; log:\n%s", log)
	}
}
