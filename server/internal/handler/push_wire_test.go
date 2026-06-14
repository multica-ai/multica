// CEREBRO-PATCH(handler-push-service-wire): regression guard for the Web Push
// handler wiring (TECH-3548). Net-new cerebro test for an upstream handler.
package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/service"
)

// TestNewWiresPushService guards against the TECH-3548 regression: handler.New
// accepted a *service.PushService argument but forgot to assign it to the
// Handler.PushService field, so h.PushService was always nil and
// /api/push/public-key reported enabled:false even with VAPID keys configured.
//
// The constructor is exercised with nil DB-facing dependencies on purpose — it
// must not touch the database to wire a value object through, and this keeps the
// test free of a Postgres fixture.
func TestNewWiresPushService(t *testing.T) {
	t.Setenv("VAPID_PUBLIC_KEY", "BIHbOmSeq7of0Ptb2AsogBcIWq7nF60R73KEhLyOmWyYscQQmU59nlT1gvlRdp1Kv-XIGpcRXI7IQ4G6IIu89as")
	t.Setenv("VAPID_PRIVATE_KEY", "eF1toZd_6lArrre-e7fehIUWbJHbURklSlUW6fy-C1s")

	pushSvc := service.NewPushService(nil)
	if !pushSvc.Enabled() {
		t.Fatal("precondition: push service should be enabled with VAPID env set")
	}

	h := New(nil, nil, nil, nil, nil, pushSvc, nil, nil, analytics.NoopClient{}, Config{})

	if h.PushService == nil {
		t.Fatal("handler.New dropped the pushService argument: h.PushService is nil")
	}
	if !h.PushService.Enabled() {
		t.Fatal("handler.PushService is not the enabled service passed to New")
	}
	if got := h.PushService.PublicKey(); got == "" {
		t.Fatal("handler.PushService.PublicKey() is empty; wiring broken")
	}
}
