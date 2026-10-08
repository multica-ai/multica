package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

type schedulePulseState struct {
	DeliveryID     string `json:"delivery_id"`
	Count          int    `json:"count"`
	LastPlannedAt  string `json:"last_planned_at"`
	LastOccurredAt string `json:"last_occurred_at"`
	LastAttempt    int    `json:"last_attempt"`
}

type schedulePulseHookBody struct {
	DeliveryID   string `json:"delivery_id"`
	InvocationID string `json:"invocation_id"`
	Attempt      int    `json:"attempt"`
	Trigger      string `json:"trigger"`
	HookKey      string `json:"hook_key"`
	OccurredAt   string `json:"occurred_at"`
	CallbackURL  string `json:"callback_url"`
	Callback     string `json:"callback_token"`
	Schedule     struct {
		PlannedAt string `json:"planned_at"`
	} `json:"schedule"`
}

func schedulePulseExampleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "examples", "plugins", "schedule-pulse")
	if _, err := os.Stat(filepath.Join(dir, plugincontract.ManifestFilename)); err != nil {
		t.Fatalf("the schedule-pulse example is missing: %v", err)
	}
	return dir
}

// TestSchedulePulseExampleManifestDrivesDurableRetry is the author-facing
// proof for MUL-6582: the shipped example plugin, not a fixture, is installed
// and woken by two scheduler replicas. The first HTTP attempt writes workspace
// storage and then drops the connection; the retry keeps the same delivery_id
// and does not increment the pulse count.
func TestSchedulePulseExampleManifestDrivesDurableRetry(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	var (
		mu            sync.Mutex
		hookCalls     int
		signingSecret string
		callbacks     = service.NewCallbackTokens()
	)

	plugins := service.NewPluginService(queries, testPool)
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	plugins.FeatureFlags = featureflag.NewService(provider)
	plugins.DeploymentKey = []byte("0123456789abcdef0123456789abcdef")
	plugins.Callbacks = callbacks

	action := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		grant, err := callbacks.Resolve(token)
		if err != nil || grant.Trigger != plugincontract.TriggerSchedule || grant.Actor.Type != "plugin" {
			http.Error(w, "callback grant", http.StatusUnauthorized)
			return
		}
		scopeID, err := service.ResolveStorageScope(service.PluginStorageWorkspace, grant.WorkspaceID, pgtype.UUID{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/storage/workspace/")
		switch {
		case r.Method == http.MethodGet:
			value, getErr := plugins.GetStorageValue(r.Context(), grant.InstallationID, service.PluginStorageWorkspace, scopeID, key)
			if getErr != nil {
				var pluginErr *service.PluginError
				if errors.As(getErr, &pluginErr) && pluginErr.Kind == service.PluginErrorNotFound {
					http.Error(w, pluginErr.Message, http.StatusNotFound)
					return
				}
				http.Error(w, getErr.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"value": value})
		case r.Method == http.MethodPut:
			var payload struct {
				Value string `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "json", http.StatusBadRequest)
				return
			}
			if err := plugins.SetStorageValue(r.Context(), grant.InstallationID, service.PluginStorageWorkspace, scopeID, key, payload.Value); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(action.Close)
	plugins.CallbackBaseURL = action.URL

	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/hooks/pulse" {
			http.NotFound(w, r)
			return
		}
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
			http.Error(w, "signature", http.StatusUnauthorized)
			return
		}
		var body schedulePulseHookBody
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		if body.HookKey != "pulse" || body.Trigger != plugincontract.TriggerSchedule || body.DeliveryID == "" {
			http.Error(w, "not a scheduled pulse", http.StatusBadRequest)
			return
		}
		req, err := http.NewRequest(http.MethodGet, body.CallbackURL+"/storage/workspace/last_pulse", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+body.Callback)
		stored, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		storedBody, _ := io.ReadAll(stored.Body)
		_ = stored.Body.Close()
		previous := schedulePulseState{}
		if stored.StatusCode == http.StatusOK {
			var wrapped struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(storedBody, &wrapped); err == nil {
				_ = json.Unmarshal([]byte(wrapped.Value), &previous)
			}
		} else if stored.StatusCode != http.StatusNotFound {
			http.Error(w, "storage get", http.StatusBadGateway)
			return
		}

		mu.Lock()
		hookCalls++
		attemptIndex := hookCalls
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if previous.DeliveryID == body.DeliveryID {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"duplicate":true,"delivery_id":%q,"count":%d}`, body.DeliveryID, previous.Count)))
			return
		}
		pulse := schedulePulseState{
			DeliveryID:     body.DeliveryID,
			Count:          previous.Count + 1,
			LastPlannedAt:  body.Schedule.PlannedAt,
			LastOccurredAt: body.OccurredAt,
			LastAttempt:    body.Attempt,
		}
		encoded, _ := json.Marshal(pulse)
		put, err := http.NewRequest(http.MethodPut, body.CallbackURL+"/storage/workspace/last_pulse", strings.NewReader(fmt.Sprintf(`{"value":%s}`, strconvQuote(string(encoded)))))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		put.Header.Set("Authorization", "Bearer "+body.Callback)
		put.Header.Set("Content-Type", "application/json")
		putResp, err := http.DefaultClient.Do(put)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		_ = putResp.Body.Close()
		if putResp.StatusCode != http.StatusNoContent {
			http.Error(w, "storage put", http.StatusBadGateway)
			return
		}
		if attemptIndex == 1 {
			connection, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr != nil {
				http.Error(w, "hijack", http.StatusInternalServerError)
				return
			}
			_ = connection.Close()
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"accepted":true,"delivery_id":%q,"count":%d}`, pulse.DeliveryID, pulse.Count)))
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
	hookOrigin := "https://pulse.example.com:" + port
	client := endpoint.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, endpoint.Listener.Addr().String())
	}
	client.Transport = transport
	plugins.DevOrigins = []string{hookOrigin}
	plugins.HookClient = client

	raw, err := os.ReadFile(filepath.Join(schedulePulseExampleDir(t), plugincontract.ManifestFilename))
	if err != nil {
		t.Fatalf("read example manifest: %v", err)
	}
	rewritten := strings.ReplaceAll(string(raw), "https://pulse.example.com", hookOrigin)
	manifest, canonical, err := plugincontract.ParseManifest([]byte(rewritten))
	if err != nil {
		t.Fatalf("example manifest does not parse: %v", err)
	}
	if err := manifest.CheckCapabilities(plugincontract.HostCapabilities()); err != nil {
		t.Fatalf("example cannot install on this host: %v", err)
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
		HookKey:        "pulse",
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
		_ = queries.DeletePluginStorageByInstallation(cleanupCtx, installation.ID)
		_ = queries.DeletePluginInvocationsByInstallation(cleanupCtx, installation.ID)
		_ = queries.DeletePluginInstallation(cleanupCtx, installation.ID)
	})

	managerA := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "schedule-pulse-a"})
	managerB := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "schedule-pulse-b"})
	for _, manager := range []*scheduler.Manager{managerA, managerB} {
		if err := manager.Register(scheduler.PluginHookScheduleDispatchJob(queries, plugins)); err != nil {
			t.Fatalf("register plugin schedule job: %v", err)
		}
	}
	runManagersTogether(t, ctx, managerA, managerB)
	if _, err := testPool.Exec(ctx, `
		UPDATE sys_cron_executions
		SET next_retry_at = now() - interval '1 second'
		WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNamePluginHookScheduleDispatch, scheduler.ScopeKindPluginHookSchedule, scopeID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	runManagersTogether(t, ctx, managerA, managerB)

	var status string
	var attempt int
	if err := testPool.QueryRow(ctx, `
		SELECT status, attempt
		FROM sys_cron_executions
		WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNamePluginHookScheduleDispatch, scheduler.ScopeKindPluginHookSchedule, scopeID).Scan(&status, &attempt); err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if status != "SUCCESS" || attempt != 2 {
		t.Fatalf("execution = %s attempt %d, want SUCCESS attempt 2", status, attempt)
	}

	mu.Lock()
	calls := hookCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("hook calls=%d, want 2", calls)
	}

	scopeIDUUID, err := service.ResolveStorageScope(service.PluginStorageWorkspace, installation.WorkspaceID, pgtype.UUID{})
	if err != nil {
		t.Fatalf("resolve storage scope: %v", err)
	}
	value, err := plugins.GetStorageValue(ctx, installation.ID, service.PluginStorageWorkspace, scopeIDUUID, "last_pulse")
	if err != nil {
		t.Fatalf("read pulse storage: %v", err)
	}
	var pulse schedulePulseState
	if err := json.Unmarshal([]byte(value), &pulse); err != nil {
		t.Fatalf("decode pulse storage: %v", err)
	}
	if pulse.Count != 1 || pulse.DeliveryID == "" {
		t.Fatalf("pulse storage = %+v, want count 1 with a delivery_id", pulse)
	}
}

func strconvQuote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}
