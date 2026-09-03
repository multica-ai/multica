package service

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func testInstallationID(t *testing.T) pgtype.UUID {
	t.Helper()
	parsed, err := parseUUIDValue("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse test uuid: %v", err)
	}
	return parsed
}

func testSigningService(t *testing.T) *PluginService {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return &PluginService{DeploymentKey: key}
}

// A signature only verifies against the exact bytes that were signed. These are
// the two forgeries the scheme exists to stop: a changed body, and a captured
// request replayed later.
func TestVerifyHookSignatureRejectsTamperedBodyAndReplay(t *testing.T) {
	service := testSigningService(t)
	installationID := testInstallationID(t)
	secret, err := service.HookSigningSecret(installationID)
	if err != nil {
		t.Fatalf("derive signing secret: %v", err)
	}

	timestamp := "1700000000"
	body := []byte(`{"hook_key":"summarize","input":{"issue_id":"abc"}}`)
	signature, err := service.SignHookPayload(installationID, timestamp, body)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	signedAt := time.Unix(1700000000, 0)

	if err := VerifyHookSignature(secret, timestamp, body, signature, signedAt); err != nil {
		t.Fatalf("a genuine request must verify: %v", err)
	}

	tampered := []byte(`{"hook_key":"summarize","input":{"issue_id":"someone-elses"}}`)
	if err := VerifyHookSignature(secret, timestamp, tampered, signature, signedAt); err == nil {
		t.Fatal("a body edited after signing must not verify")
	}

	// Same bytes, same signature, hours later. Without the timestamp in the
	// signed string a captured request would stay valid forever.
	late := signedAt.Add(2 * time.Hour)
	if err := VerifyHookSignature(secret, timestamp, body, signature, late); err == nil {
		t.Fatal("a replayed request outside the tolerance window must not verify")
	}
}

// The signing secret is per installation. Two installations on the same
// deployment must not be able to sign for each other.
func TestHookSigningSecretIsPerInstallation(t *testing.T) {
	service := testSigningService(t)
	first := testInstallationID(t)
	second, err := parseUUIDValue("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}

	firstSecret, err := service.HookSigningSecret(first)
	if err != nil {
		t.Fatalf("derive first: %v", err)
	}
	secondSecret, err := service.HookSigningSecret(second)
	if err != nil {
		t.Fatalf("derive second: %v", err)
	}
	if firstSecret == secondSecret {
		t.Fatal("two installations must not share a signing secret")
	}

	timestamp := "1700000000"
	body := []byte(`{"hook_key":"k"}`)
	signature, err := service.SignHookPayload(first, timestamp, body)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyHookSignature(secondSecret, timestamp, body, signature, time.Unix(1700000000, 0)); err == nil {
		t.Fatal("one installation's signature must not verify against another's secret")
	}
}

// Derivation is deterministic: an author configures the secret once and it keeps
// working across restarts, because nothing about it is stored.
func TestHookSigningSecretIsStableAcrossServiceInstances(t *testing.T) {
	installationID := testInstallationID(t)
	first, err := testSigningService(t).HookSigningSecret(installationID)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	second, err := testSigningService(t).HookSigningSecret(installationID)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if first != second {
		t.Fatal("the same deployment key and installation must derive the same secret")
	}
	if !strings.HasPrefix(first, "whsec_") {
		t.Fatalf("signing secret should be prefixed for recognisability, got %q", first)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(first, "whsec_")); err != nil {
		t.Fatalf("signing secret body must be hex: %v", err)
	}
}

// Without a deployment key there is nothing to sign with, and the engine must
// say so rather than send an unsigned request a handler would have to trust.
func TestHookSigningFailsClosedWithoutDeploymentKey(t *testing.T) {
	service := &PluginService{}
	if _, err := service.SignHookPayload(testInstallationID(t), "1700000000", []byte("{}")); err == nil {
		t.Fatal("signing without a deployment key must fail")
	}
}

// A hook may only be invoked through a trigger its manifest declared. The host
// does not get to invent a call site.
func TestHookAllowsTriggerOnlyWhenDeclared(t *testing.T) {
	hook := plugincontract.Hook{Triggers: []string{plugincontract.TriggerManual}}
	if !HookAllowsTrigger(hook, plugincontract.TriggerManual) {
		t.Fatal("a declared trigger must be allowed")
	}
	for _, undeclared := range []string{plugincontract.TriggerUI, plugincontract.TriggerEvent, plugincontract.TriggerAgent, plugincontract.TriggerSchedule} {
		if HookAllowsTrigger(hook, undeclared) {
			t.Fatalf("%s was not declared and must not be allowed", undeclared)
		}
	}
}

func TestScheduledHookBodyExtendsVersionOneProtocol(t *testing.T) {
	installationID := testInstallationID(t)
	invocationID := parseUUIDValueForTest(t, "22222222-2222-4222-8222-222222222222")
	plannedAt := time.Date(2026, 8, 23, 10, 15, 0, 0, time.UTC)
	body, err := (&PluginService{}).buildHookBody(context.Background(), HookInvocation{
		ID: invocationID,
		Installation: db.PluginInstallation{
			ID:          installationID,
			WorkspaceID: parseUUIDValueForTest(t, "33333333-3333-4333-8333-333333333333"),
		},
		Hook:       plugincontract.Hook{Key: "digest"},
		Trigger:    plugincontract.TriggerSchedule,
		Actor:      HookActor{Type: "plugin", ID: installationID},
		DeliveryID: "psd_stable",
		PlannedAt:  pgtype.Timestamptz{Time: plannedAt, Valid: true},
		Attempt:    2,
	})
	if err != nil {
		t.Fatalf("build scheduled hook body: %v", err)
	}
	if body.Version != 1 {
		t.Fatalf("version=%d, want existing protocol version 1", body.Version)
	}
	if body.InvocationID != uuidString(invocationID) || body.DeliveryID != "psd_stable" || body.Attempt != 2 {
		t.Fatalf("unexpected identity fields: %+v", body)
	}
	if body.OccurredAt.IsZero() {
		t.Fatal("occurred_at must be populated per request")
	}
	if body.Schedule == nil || !body.Schedule.PlannedAt.Equal(plannedAt) {
		t.Fatalf("schedule.planned_at=%v, want %s", body.Schedule, plannedAt)
	}
}

func parseUUIDValueForTest(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := parseUUIDValue(value)
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return parsed
}

// FindHook reads the CONSENTED manifest on the installation row. A hook that is
// not in it does not exist, whatever the source URL serves today.
func TestFindHookReadsTheConsentedManifest(t *testing.T) {
	installation := db.PluginInstallation{
		Manifest: []byte(`{
			"manifest_version": 1,
			"key": "com.example.hooks",
			"name": "Hooks",
			"description": "d",
			"version": "1.0.0",
			"author": {"name": "example"},
			"scopes": ["net:example.com"],
			"contributes": {"hooks": [{
				"key": "summarize",
				"name": "Summarize",
				"description": "Summarize the thread.",
				"triggers": ["manual"],
				"transport": {"type": "http", "url": "https://example.com/hooks/summarize"}
			}]}
		}`),
	}

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("declared hook must resolve: %v", err)
	}
	if hook.Transport.URL != "https://example.com/hooks/summarize" {
		t.Fatalf("unexpected transport url %q", hook.Transport.URL)
	}
	if _, err := FindHook(installation, "not_declared"); err == nil {
		t.Fatal("an undeclared hook key must not resolve")
	}
}
