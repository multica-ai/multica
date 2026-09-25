package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/scheduler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

type receivedScheduledHook struct {
	InvocationID string `json:"invocation_id"`
	DeliveryID   string `json:"delivery_id"`
	Attempt      int    `json:"attempt"`
	Trigger      string `json:"trigger"`
	Callback     string `json:"callback_token"`
	Actor        struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"actor"`
	Schedule struct {
		PlannedAt time.Time `json:"planned_at"`
	} `json:"schedule"`
}

// TestPluginHookScheduleTwoReplicasRetryWithStableDelivery is the end-to-end
// acceptance path for durable scheduled Hooks. Two managers race the same plan,
// the endpoint loses the connection after receiving attempt one, and the retry
// is reclaimed by the same DB execution row with the same delivery_id and a
// fresh invocation.
func TestPluginHookScheduleTwoReplicasRetryWithStableDelivery(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	var (
		mu              sync.Mutex
		received        []receivedScheduledHook
		verificationErr error
		signingSecret   string
		callbacks       = service.NewCallbackTokens()
	)
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		mu.Lock()
		secret := signingSecret
		mu.Unlock()
		if err := service.VerifyHookSignature(
			secret,
			r.Header.Get("X-Multica-Timestamp"),
			raw,
			r.Header.Get("X-Multica-Signature"),
			time.Now(),
		); err != nil {
			mu.Lock()
			verificationErr = err
			mu.Unlock()
			http.Error(w, "signature", http.StatusUnauthorized)
			return
		}
		var body receivedScheduledHook
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		grant, err := callbacks.Resolve(body.Callback)
		if err != nil || grant.Trigger != plugincontract.TriggerSchedule ||
			grant.Actor.Type != "plugin" || grant.IssueID.Valid {
			mu.Lock()
			verificationErr = fmt.Errorf("callback grant mismatch: grant=%+v err=%v", grant, err)
			mu.Unlock()
			http.Error(w, "callback grant", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		received = append(received, body)
		attemptIndex := len(received)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if attemptIndex == 1 {
			// Model the irreducible uncertain-result case: the Plugin received
			// and may have committed the delivery, but the Host sees no response.
			connection, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr != nil {
				http.Error(w, "hijack", http.StatusInternalServerError)
				return
			}
			_ = connection.Close()
			return
		}
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	t.Cleanup(endpoint.Close)

	parsedEndpoint, err := url.Parse(endpoint.URL)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	_, port, err := net.SplitHostPort(parsedEndpoint.Host)
	if err != nil {
		t.Fatalf("split endpoint: %v", err)
	}
	hookOrigin := "https://hooks.example.com:" + port
	hookURL := hookOrigin + "/hooks/scheduled"

	client := endpoint.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, endpoint.Listener.Addr().String())
	}
	client.Transport = transport

	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	plugins := service.NewPluginService(queries, testPool)
	plugins.FeatureFlags = featureflag.NewService(provider)
	plugins.DeploymentKey = []byte("0123456789abcdef0123456789abcdef")
	plugins.Callbacks = callbacks
	plugins.CallbackBaseURL = "https://plugin-api.example.com/v1"
	plugins.DevOrigins = []string{hookOrigin}
	plugins.HookClient = client

	manifestRaw := []byte(fmt.Sprintf(`{
		"manifest_version":1,
		"key":"com.example.schedule-test",
		"name":"Schedule Test",
		"description":"test",
		"version":"1.0.0",
		"author":{"name":"test"},
		"scopes":["net:hooks.example.com"],
		"contributes":{"hooks":[{
			"key":"scheduled",
			"name":"Scheduled",
			"description":"Scheduled integration test hook.",
			"triggers":["schedule"],
			"schedule":{"cron":"*/5 * * * *","timezone":"UTC"},
			"transport":{"type":"http","url":%s},
			"timeout_ms":1000
		}]}
	}`, strconv.Quote(hookURL)))
	manifest, canonical, err := plugincontract.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatalf("parse fixture manifest: %v", err)
	}
	grantedScopes, _ := json.Marshal(manifest.Scopes)
	installation, err := queries.CreatePluginInstallation(ctx, db.CreatePluginInstallationParams{
		WorkspaceID:      parseUUID(testWorkspaceID),
		PluginKey:        manifest.Key,
		PackageVersionID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Version:          manifest.Version,
		Manifest:         canonical,
		GrantedScopes:    grantedScopes,
		InstalledBy:      parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create plugin installation: %v", err)
	}
	derivedSigningSecret, err := plugins.HookSigningSecret(installation.ID)
	if err != nil {
		t.Fatalf("derive signing secret: %v", err)
	}
	mu.Lock()
	signingSecret = derivedSigningSecret
	mu.Unlock()
	scheduleRow, err := queries.CreatePluginHookSchedule(ctx, db.CreatePluginHookScheduleParams{
		InstallationID: installation.ID,
		WorkspaceID:    installation.WorkspaceID,
		HookKey:        "scheduled",
		CronExpression: "*/5 * * * *",
		Timezone:       "UTC",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("create plugin schedule: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE plugin_hook_schedule SET activated_at = now() - interval '6 minutes' WHERE id = $1`,
		scheduleRow.ID,
	); err != nil {
		t.Fatalf("backdate schedule: %v", err)
	}
	scopeID := util.UUIDToString(scheduleRow.ID) + ":" + util.UUIDToString(scheduleRow.Generation)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = testPool.Exec(cleanupCtx,
			`DELETE FROM sys_cron_executions WHERE job_name = $1 AND scope_id = $2`,
			scheduler.JobNamePluginHookScheduleDispatch, scopeID,
		)
		_ = queries.DeletePluginHookSchedulesByInstallation(cleanupCtx, installation.ID)
		_ = queries.DeletePluginInvocationsByInstallation(cleanupCtx, installation.ID)
		_ = queries.DeletePluginInstallation(cleanupCtx, installation.ID)
	})

	managerA := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "plugin-schedule-a"})
	managerB := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "plugin-schedule-b"})
	for _, manager := range []*scheduler.Manager{managerA, managerB} {
		if err := manager.Register(scheduler.PluginHookScheduleDispatchJob(queries, plugins)); err != nil {
			t.Fatalf("register plugin schedule job: %v", err)
		}
	}
	runManagersTogether(t, ctx, managerA, managerB)

	var status string
	var attempt int
	if err := testPool.QueryRow(ctx, `
		SELECT status, attempt
		FROM sys_cron_executions
		WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNamePluginHookScheduleDispatch, scheduler.ScopeKindPluginHookSchedule, scopeID).Scan(&status, &attempt); err != nil {
		t.Fatalf("load failed execution: %v", err)
	}
	if status != "FAILED" || attempt != 1 {
		t.Fatalf("first execution = %s attempt %d, want FAILED attempt 1", status, attempt)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE sys_cron_executions
		SET next_retry_at = now() - interval '1 second'
		WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNamePluginHookScheduleDispatch, scheduler.ScopeKindPluginHookSchedule, scopeID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	runManagersTogether(t, ctx, managerA, managerB)

	if err := testPool.QueryRow(ctx, `
		SELECT status, attempt
		FROM sys_cron_executions
		WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNamePluginHookScheduleDispatch, scheduler.ScopeKindPluginHookSchedule, scopeID).Scan(&status, &attempt); err != nil {
		t.Fatalf("load successful retry: %v", err)
	}
	if status != "SUCCESS" || attempt != 2 {
		t.Fatalf("retried execution = %s attempt %d, want SUCCESS attempt 2", status, attempt)
	}

	mu.Lock()
	requests := append([]receivedScheduledHook(nil), received...)
	verifyErr := verificationErr
	mu.Unlock()
	if verifyErr != nil {
		t.Fatalf("endpoint rejected HMAC: %v", verifyErr)
	}
	if len(requests) != 2 {
		t.Fatalf("endpoint calls=%d, want one failed attempt and one retry", len(requests))
	}
	if requests[0].DeliveryID == "" || requests[0].DeliveryID != requests[1].DeliveryID {
		t.Fatalf("delivery ids changed across retry: %q != %q", requests[0].DeliveryID, requests[1].DeliveryID)
	}
	if requests[0].InvocationID == requests[1].InvocationID {
		t.Fatal("invocation_id must be unique per HTTP attempt")
	}
	if requests[0].Attempt != 1 || requests[1].Attempt != 2 {
		t.Fatalf("attempts=%d,%d, want 1,2", requests[0].Attempt, requests[1].Attempt)
	}
	if requests[1].Trigger != plugincontract.TriggerSchedule ||
		requests[1].Actor.Type != "plugin" || requests[1].Actor.ID != util.UUIDToString(installation.ID) {
		t.Fatalf("scheduled actor/trigger mismatch: %+v", requests[1])
	}
	if requests[0].Callback == "" || requests[1].Callback == "" || requests[0].Callback == requests[1].Callback {
		t.Fatal("each attempt must receive a fresh callback token")
	}
	if _, err := plugins.Callbacks.Resolve(requests[1].Callback); err == nil {
		t.Fatal("callback token must be revoked after the endpoint returns")
	}

	var invocationCount int
	var distinctDeliveries int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT delivery_id)
		FROM plugin_invocation
		WHERE installation_id = $1 AND trigger = 'schedule'
	`, installation.ID).Scan(&invocationCount, &distinctDeliveries); err != nil {
		t.Fatalf("load invocation audit: %v", err)
	}
	if invocationCount != 2 || distinctDeliveries != 1 {
		t.Fatalf("invocation audit has %d rows/%d deliveries, want 2/1", invocationCount, distinctDeliveries)
	}
}

func runManagersTogether(t *testing.T, ctx context.Context, managers ...*scheduler.Manager) {
	t.Helper()
	errs := make(chan error, len(managers))
	var wg sync.WaitGroup
	for _, manager := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- manager.RunOnce(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("scheduler tick: %v", err)
		}
	}
}
