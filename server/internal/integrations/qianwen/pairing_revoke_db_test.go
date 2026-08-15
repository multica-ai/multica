package qianwen

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRevokeQianwenInstallationAtomicallyRemovesPairingAuthority(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.UnixMilli(1786723200000)
	fixture.service.now = func() time.Time { return now }

	invalid := PairingRedeemRequest{
		Code: "99999999",
		Identity: InvocationMetadata{
			OpenUserID: "opaque-revoke-invalid-user",
			OpenUUID:   "opaque-revoke-invalid-device",
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "revoke-invalid-nonce-00000000001",
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &invalid)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, invalid); !errors.Is(err, ErrPairingCodeInvalid) {
		t.Fatalf("invalid RedeemPairingCode() error = %v, want ErrPairingCodeInvalid", err)
	}

	minted, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID)
	if err != nil {
		t.Fatalf("MintPairingCode() error = %v", err)
	}
	paired := PairingRedeemRequest{
		Code: minted.Code,
		Identity: InvocationMetadata{
			OpenUserID: "opaque-revoke-paired-user",
			OpenUUID:   "opaque-revoke-paired-device",
			Timestamp:  fmt.Sprint(now.Add(time.Second).UnixMilli()),
			Nonce:      "revoke-paired-nonce-000000000001",
		},
	}
	signPairingRedeemRequest(fixture.installation.AccessToken, &paired)
	if _, err := fixture.service.RedeemPairingCode(ctx, fixture.installation.ConnectionID, fixture.installation.AccessToken, paired); err != nil {
		t.Fatalf("paired RedeemPairingCode() error = %v", err)
	}
	if _, err := fixture.service.MintPairingCode(ctx, fixture.workspaceID, fixture.installation.Installation.ID, fixture.userID); err != nil {
		t.Fatalf("pending MintPairingCode() error = %v", err)
	}

	if err := fixture.service.Revoke(ctx, fixture.workspaceID, fixture.installation.Installation.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	var status string
	var codes, attempts, nonces, bindings int
	if err := fixture.pool.QueryRow(ctx, `SELECT status FROM channel_installation WHERE id = $1`, fixture.installation.Installation.ID).Scan(&status); err != nil {
		t.Fatalf("load revoked installation: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM qianwen_pairing_code WHERE installation_id = $1`, fixture.installation.Installation.ID).Scan(&codes); err != nil {
		t.Fatalf("count pairing codes: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM qianwen_pairing_attempt WHERE installation_id = $1`, fixture.installation.Installation.ID).Scan(&attempts); err != nil {
		t.Fatalf("count pairing attempts: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM qianwen_invocation_nonce WHERE installation_id = $1`, fixture.installation.Installation.ID).Scan(&nonces); err != nil {
		t.Fatalf("count invocation outcomes: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM channel_user_binding WHERE installation_id = $1`, fixture.installation.Installation.ID).Scan(&bindings); err != nil {
		t.Fatalf("count user bindings: %v", err)
	}
	if status != "revoked" || codes != 0 || attempts != 0 || nonces != 0 || bindings != 0 {
		t.Fatalf("revoked state status=%q codes=%d attempts=%d nonces=%d bindings=%d, want revoked and all pairing authority removed", status, codes, attempts, nonces, bindings)
	}
}

func TestRevokeQianwenInstallationRejectsWrongWorkspace(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wrongWorkspaceID := fixture.workspaceID
	wrongWorkspaceID.Bytes[0] ^= 0xff
	if err := fixture.service.Revoke(ctx, wrongWorkspaceID, fixture.installation.Installation.ID); !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("Revoke() error = %v, want ErrInstallationNotFound", err)
	}

	installation, err := fixture.service.GetInWorkspace(ctx, fixture.installation.Installation.ID, fixture.workspaceID)
	if err != nil {
		t.Fatalf("GetInWorkspace() after rejected revoke error = %v", err)
	}
	if installation.Status != "active" {
		t.Fatalf("installation status = %q, want active", installation.Status)
	}
}
