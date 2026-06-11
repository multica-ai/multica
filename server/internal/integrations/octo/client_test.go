package octo_test

import (
	"context"
	"crypto/rand"
	"testing"

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
