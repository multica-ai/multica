package lark

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSendObservationDatabase(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Send observation", "observation-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Send observation", "observation-"+suffix)
	runtime := fx.Runtime(t, "Observation runtime")
	agent := fx.Agent(t, "Observation agent", runtime)
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(pool)
	svc, err := NewInstallationService(q, box)
	if err != nil {
		t.Fatal(err)
	}
	params := InstallationParams{WorkspaceID: util.MustParseUUID(fx.WorkspaceID), AgentID: util.MustParseUUID(agent), InstallerUserID: util.MustParseUUID(fx.UserID), AppID: "cli_observation_" + suffix, AppSecret: "test", BotOpenID: "ou_bot"}
	ctx := context.Background()
	inst, err := svc.Upsert(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	fx.Cleanup(t, "DELETE FROM channel_installation WHERE id=$1", inst.ID)
	read := func() Installation {
		t.Helper()
		got, err := svc.GetInWorkspace(ctx, inst.ID, inst.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	old := classifySendObservation(time.Date(2026, 11, 2, 0, 0, 0, 1, time.UTC), "id", nil)
	newer := classifySendObservation(time.Date(2026, 11, 2, 0, 0, 0, 2, time.UTC), "", &APIError{Code: 230027})
	if read().LastSendObservation != nil {
		t.Fatal("new binding has observation")
	}
	if err := svc.RecordSendObservation(ctx, inst, newer); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordSendObservation(ctx, inst, old); err != nil {
		t.Fatal(err)
	}
	got := read()
	if got.LastSendObservation == nil || *got.LastSendObservation != newer {
		t.Fatal("late write replaced newer nanosecond observation")
	}
	if !got.UpdatedAt.Time.Equal(inst.UpdatedAt.Time) {
		t.Fatal("observation changed installation updated_at")
	}

	// All three writers start together on the same JSONB config. None may
	// replace another field or turn a send observation into a connection change.
	newer.ObservedAt = "2026-11-02T00:00:00.000000003Z"
	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for _, write := range []func() error{
		func() error { return svc.RecordSendObservation(ctx, inst, newer) },
		func() error {
			return q.RecordFeishuInboundReceipt(ctx, db.RecordFeishuInboundReceiptParams{ID: inst.ID, WorkspaceID: inst.WorkspaceID, AppID: inst.AppID, BindingGeneration: inst.BindingGeneration})
		},
		func() error {
			return q.SetFeishuInstallationBotUnionID(ctx, db.SetFeishuInstallationBotUnionIDParams{ID: inst.ID, BotUnionID: "on_observed"})
		},
	} {
		wg.Add(1)
		go func(write func() error) { defer wg.Done(); <-start; errs <- write() }(write)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got = read()
	if got.LastSendObservation == nil || *got.LastSendObservation != newer || got.FirstMessageReceivedAt == nil || got.BotUnionID.String != "on_observed" || got.BindingGeneration != inst.BindingGeneration {
		t.Fatal("concurrent config writes lost evidence or identity")
	}

	for _, scope := range []string{"workspace", "app", "generation", "inactive", "channel"} {
		t.Run(scope, func(t *testing.T) {
			snapshot := inst
			switch scope {
			case "workspace":
				snapshot.WorkspaceID = util.MustParseUUID(uuid.NewString())
			case "app":
				snapshot.AppID = "other"
			case "generation":
				snapshot.BindingGeneration = "other"
			case "inactive":
				fx.Exec(t, "UPDATE channel_installation SET status='revoked' WHERE id=$1", inst.ID)
			case "channel":
				fx.Exec(t, "UPDATE channel_installation SET channel_type='slack' WHERE id=$1", inst.ID)
			}
			future := classifySendObservation(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "id", nil)
			if err := svc.RecordSendObservation(ctx, snapshot, future); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			if err := pool.QueryRow(ctx, "SELECT config->'last_send_observation' FROM channel_installation WHERE id=$1", inst.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var o SendObservation
			if err := json.Unmarshal(raw, &o); err != nil {
				t.Fatal(err)
			}
			if o != newer {
				t.Fatal("out-of-scope observation accepted")
			}
			fx.Exec(t, "UPDATE channel_installation SET channel_type='feishu', status='active' WHERE id=$1", inst.ID)
		})
	}
	rebound, err := svc.Upsert(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.BindingGeneration == inst.BindingGeneration || read().LastSendObservation != nil {
		t.Fatal("rebind retained observation or generation")
	}
	if err := svc.RecordSendObservation(ctx, inst, newer); err != nil {
		t.Fatal(err)
	}
	if read().LastSendObservation != nil {
		t.Fatal("old send wrote into a new binding")
	}
}

func TestSendObservationFingerprint(t *testing.T) {
	row := db.ChannelInstallation{ChannelType: channelTypeFeishu, Config: []byte(`{"app_id":"app","binding_generation":"one"}`)}
	before := rowFingerprint(row)
	row.Config = []byte(`{"app_id":"app","binding_generation":"one","last_send_observation":{"result":"rejected","observed_at":"2026-11-02T00:00:00.000000001Z","reason":"missing_permission"}}`)
	if rowFingerprint(row) != before {
		t.Fatal("observation restarted connection")
	}
	row.Config = []byte(`{"app_id":"app","binding_generation":"two"}`)
	if rowFingerprint(row) == before {
		t.Fatal("generation change did not restart connection")
	}
	row.Config = []byte(`{"app_id":"other","binding_generation":"one"}`)
	if rowFingerprint(row) == before {
		t.Fatal("credential change did not restart connection")
	}
}
