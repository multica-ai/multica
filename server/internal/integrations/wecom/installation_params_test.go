package wecom

// installation_params_test.go — the HTTP layer decides between "the admin
// left a field out" (their fix) and "something broke on our side" (not their
// fix) by matching on ErrInvalidInstallationParams. If a validation failure
// stops matching, that request silently becomes a 500; if a real failure
// starts matching, the admin gets sent to rotate a secret that was fine.

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func validParams() InstallationParams {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	return InstallationParams{
		WorkspaceID:     id,
		AgentID:         id,
		InstallerUserID: id,
		BotID:           "bot-1",
		Secret:          "s3cret",
	}
}

func TestEveryMissingFieldIsTheCallersToFix(t *testing.T) {
	cases := map[string]func(*InstallationParams){
		"workspace_id":      func(p *InstallationParams) { p.WorkspaceID = pgtype.UUID{} },
		"agent_id":          func(p *InstallationParams) { p.AgentID = pgtype.UUID{} },
		"installer_user_id": func(p *InstallationParams) { p.InstallerUserID = pgtype.UUID{} },
		"bot_id":            func(p *InstallationParams) { p.BotID = "" },
		"secret":            func(p *InstallationParams) { p.Secret = "" },
	}
	for field, blank := range cases {
		p := validParams()
		blank(&p)
		err := validateInstallationParams(p)
		if err == nil {
			t.Errorf("%s: missing field accepted", field)
			continue
		}
		if !errors.Is(err, ErrInvalidInstallationParams) {
			t.Errorf("%s: error does not match ErrInvalidInstallationParams, so the handler will answer 500 instead of 400: %v", field, err)
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("%s: error does not name the field: %v", field, err)
		}
	}
}

func TestCompleteParamsPass(t *testing.T) {
	if err := validateInstallationParams(validParams()); err != nil {
		t.Fatalf("complete params rejected: %v", err)
	}
}
