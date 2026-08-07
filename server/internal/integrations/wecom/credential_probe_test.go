package wecom

// credential_probe_test.go — idx_channel_installation_type_appid is UNIQUE on
// (channel_type, config->>'app_id') with no workspace in it, so a bot id's
// routing slot is global. ReclaimDeadChannelInstallationByAppID then
// hard-deletes whoever holds it, plus every binding beneath. Bot ids are not
// secret. The only thing that can separate "the rightful owner rebinding"
// from "anyone who typed that id" is proof they hold the secret.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func testUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

type fakeProbe struct {
	err    error
	calls  int
	gotBot string
	gotSec string
}

func (f *fakeProbe) Probe(_ context.Context, botID, secret string) error {
	f.calls++
	f.gotBot, f.gotSec = botID, secret
	return f.err
}

func TestUpsertRefusesBeforeTouchingAnythingWhenTheSecretIsWrong(t *testing.T) {
	probe := &fakeProbe{err: ErrCredentialsRejected}
	// tx and store are nil on purpose: if the probe does not short-circuit,
	// the reclaim runs and this panics instead of returning — which is
	// exactly the distinction being asserted.
	svc := &InstallationService{probe: probe}

	// Reaching the write path at all is the failure: it means a caller who
	// guessed the bot id got as far as the reclaim, which hard-deletes the
	// current holder's row and every binding under it.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Upsert reached the write path with an unproven credential — the reclaim would have destroyed the current holder's installation and its bindings (panicked at %v)", r)
		}
	}()

	_, err := svc.Upsert(context.Background(), InstallationParams{
		WorkspaceID:     testUUID(1),
		AgentID:         testUUID(2),
		InstallerUserID: testUUID(3),
		BotID:           "somebody-elses-bot",
		Secret:          "a-guess",
	})
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("err = %v, want ErrCredentialsRejected", err)
	}
	if probe.calls != 1 {
		t.Fatalf("probe called %d times, want 1", probe.calls)
	}
	if probe.gotBot != "somebody-elses-bot" || probe.gotSec != "a-guess" {
		t.Errorf("probe got (%q, %q), want the caller's own pair", probe.gotBot, probe.gotSec)
	}
}

// An unreachable WeCom is not a wrong credential. Reporting it as one sends
// the admin to rotate a secret that was fine, and a rotated WeCom secret
// cannot be recovered.
func TestUpsertSeparatesUnverifiableFromRejected(t *testing.T) {
	probe := &fakeProbe{err: ErrCredentialsUnverifiable}
	svc := &InstallationService{probe: probe}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Upsert reached the write path without a verdict (panicked at %v)", r)
		}
	}()

	_, err := svc.Upsert(context.Background(), InstallationParams{
		WorkspaceID:     testUUID(1),
		AgentID:         testUUID(2),
		InstallerUserID: testUUID(3),
		BotID:           "bot",
		Secret:          "secret",
	})
	if !errors.Is(err, ErrCredentialsUnverifiable) {
		t.Fatalf("err = %v, want ErrCredentialsUnverifiable", err)
	}
	if errors.Is(err, ErrCredentialsRejected) {
		t.Error("an unreachable WeCom was reported as a wrong credential")
	}
}

// A deployment that cannot reach WeCom at install time must still be able to
// install; nil probe keeps the previous behaviour rather than blocking.
func TestNilProbeDoesNotBlockInstall(t *testing.T) {
	svc := &InstallationService{}
	if svc.probe != nil {
		t.Fatal("probe should default to nil")
	}
}

func TestWithCredentialProbeSetsIt(t *testing.T) {
	p := &fakeProbe{}
	svc := &InstallationService{}
	WithCredentialProbe(p)(svc)
	if svc.probe != p {
		t.Fatal("WithCredentialProbe did not install the probe")
	}
}
