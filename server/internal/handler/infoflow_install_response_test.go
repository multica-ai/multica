package handler

// infoflow_install_response_test.go pins the installation wire shape the
// Settings UI parses. Two things must hold together:
//
//   - app_key IS on the wire. The frontend type declares it, the management UI
//     shows it as the installation's name, and a response that omits it would
//     render every robot as "Not connected"-adjacent blank rows.
//   - the sealed AppSecret is NOT. config is server-internal; leaking it would
//     hand every workspace member the robot credential.
//
// The two are stored side by side in channel_installation.config, so a change to
// how that blob is decoded can flip either one without touching a signature.

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestInfoFlowInstallationResponseExposesAppKeyWithoutTheSealedSecret(t *testing.T) {
	// base64 of a plausible secretbox blob; the handler never opens it, so the
	// bytes only need to survive a JSON round trip.
	sealed := base64.StdEncoding.EncodeToString([]byte("sealed-ciphertext"))

	row := db.ChannelInstallation{
		Config: mustJSONBytes(t, map[string]string{
			"app_id":               "infoflow-app-key-7",
			"app_secret_encrypted": sealed,
		}),
		Status:          "active",
		InstallerUserID: pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true},
	}

	got := infoflowInstallationToResponse(row)
	if got.AppKey != "infoflow-app-key-7" {
		t.Errorf("AppKey = %q, want infoflow-app-key-7", got.AppKey)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, sealed) {
		t.Errorf("response leaks the sealed AppSecret: %s", body)
	}
	if strings.Contains(body, "app_secret") {
		t.Errorf("response exposes the app_secret field name: %s", body)
	}
	if !strings.Contains(body, `"app_key":"infoflow-app-key-7"`) {
		t.Errorf("response is missing app_key: %s", body)
	}
}

func TestInfoFlowInstallationResponseToleratesAnUndecodableConfig(t *testing.T) {
	// A row written by an older build (or one whose config was truncated) must
	// still list: an empty app_key is a display gap, a 500 on the list endpoint
	// would hide every other installation too.
	got := infoflowInstallationToResponse(db.ChannelInstallation{
		Config: []byte("{not json"),
		Status: "active",
	})
	if got.AppKey != "" {
		t.Errorf("AppKey = %q, want empty", got.AppKey)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want active", got.Status)
	}
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
