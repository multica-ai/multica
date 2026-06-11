package octo_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/octo"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newBox(t *testing.T) *secretbox.Box {
	t.Helper()
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	box, err := secretbox.New(key[:])
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func TestInstallationService_UpsertDecryptRoundTrip(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	ctx := context.Background()

	svc, err := octo.NewInstallationService(q, newBox(t))
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}

	const token = "bf_secret_token_value"
	inst, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID:     wsID,
		AgentID:         agentID,
		BotToken:        token,
		RobotID:         "robot_" + randToken(),
		BotName:         "Octo-Z",
		OwnerUID:        "owner_x",
		APIURL:          "https://im.example/api",
		WSURL:           "wss://im.example/ws",
		InstallerUserID: userID,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Ciphertext must not be the plaintext.
	if string(inst.BotTokenEncrypted) == token {
		t.Fatal("bot token stored in plaintext")
	}

	// DecryptBotToken round-trips to the original.
	got, err := svc.DecryptBotToken(inst)
	if err != nil {
		t.Fatalf("DecryptBotToken: %v", err)
	}
	if got != token {
		t.Errorf("decrypted token = %q, want %q", got, token)
	}
}

func TestInstallationService_NilBoxRejected(t *testing.T) {
	if _, err := octo.NewInstallationService(nil, nil); err == nil {
		t.Error("expected error for nil secretbox.Box")
	}
}

// TestInstallationService_RobotAlreadyBound guards the fix for the 500 an admin
// hit when binding a bot whose robot_id is already in use by a different agent.
// The deployment-wide UNIQUE(robot_id) constraint must surface as the typed
// ErrRobotAlreadyBound (→ 409), not a raw DB error (→ 500).
func TestInstallationService_RobotAlreadyBound(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	ctx := context.Background()
	svc, _ := octo.NewInstallationService(q, newBox(t))

	robotID := "robot_" + randToken()
	if _, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID: wsID, AgentID: agentID, BotToken: "bf_a",
		RobotID: robotID, APIURL: "https://im.example/api", InstallerUserID: userID,
	}); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	// A second agent in the same workspace trying to bind the SAME bot
	// (same robot_id) must be rejected with the typed error — the upsert's
	// ON CONFLICT only covers (workspace_id, agent_id), so this is an INSERT
	// that trips UNIQUE(robot_id).
	var agentID2 pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 SELECT $1, 'Octo Agent 2', 'local', runtime_id FROM agent WHERE id = $2
		 RETURNING id`,
		wsID, agentID).Scan(&agentID2); err != nil {
		t.Fatalf("create second agent: %v", err)
	}

	_, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID: wsID, AgentID: agentID2, BotToken: "bf_b",
		RobotID: robotID, APIURL: "https://im.example/api", InstallerUserID: userID,
	})
	if !errors.Is(err, octo.ErrRobotAlreadyBound) {
		t.Fatalf("second Upsert error = %v, want ErrRobotAlreadyBound", err)
	}
}

// TestInstallationService_ReconfigureSameAgentSucceeds confirms the fix did not
// break the legitimate re-configure path: re-binding the SAME (workspace, agent)
// with the same robot_id hits ON CONFLICT DO UPDATE and must succeed.
func TestInstallationService_ReconfigureSameAgentSucceeds(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	ctx := context.Background()
	svc, _ := octo.NewInstallationService(q, newBox(t))

	robotID := "robot_" + randToken()
	first, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID: wsID, AgentID: agentID, BotToken: "bf_a",
		RobotID: robotID, APIURL: "https://im.example/api", InstallerUserID: userID,
	})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	second, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID: wsID, AgentID: agentID, BotToken: "bf_rotated",
		RobotID: robotID, APIURL: "https://im.example/api", InstallerUserID: userID,
	})
	if err != nil {
		t.Fatalf("reconfigure Upsert: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("reconfigure created a new row (%v → %v); expected in-place update", first.ID, second.ID)
	}
	if got, _ := svc.DecryptBotToken(second); got != "bf_rotated" {
		t.Errorf("reconfigure did not rotate token, got %q", got)
	}
}

func TestInstallationService_Revoke(t *testing.T) {
	requireDB(t)
	q := db.New(testPool)
	wsID, userID, agentID := fixture(t)
	ctx := context.Background()
	svc, _ := octo.NewInstallationService(q, newBox(t))

	inst, err := svc.Upsert(ctx, octo.InstallationParams{
		WorkspaceID: wsID, AgentID: agentID, BotToken: "bf_x",
		RobotID: "robot_" + randToken(), APIURL: "https://im.example/api",
		InstallerUserID: userID,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := svc.Revoke(ctx, inst.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	active, _ := q.ListActiveOctoInstallations(ctx)
	if containsInstallation(active, inst.ID) {
		t.Error("revoked installation still active")
	}
}
