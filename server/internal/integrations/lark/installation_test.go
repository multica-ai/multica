package lark

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func memberInstallationUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		t.Fatalf("parse UUID: %v", err)
	}
	return id
}

func TestValidateInstallationParamsMemberRequiresMemberUser(t *testing.T) {
	params := InstallationParams{
		WorkspaceID:     memberInstallationUUID(t, "00000000-0000-0000-0000-000000000001"),
		InstallerUserID: memberInstallationUUID(t, "00000000-0000-0000-0000-000000000002"),
		Kind:            InstallationKindMember,
		AppID:           "cli_member",
		AppSecret:       "secret",
		BotOpenID:       "ou_bot",
	}
	if err := validateInstallationParams(params); err == nil || err.Error() != "member_user_id is required" {
		t.Fatalf("validateInstallationParams(member without user) = %v, want member_user_id error", err)
	}

	params.MemberUserID = memberInstallationUUID(t, "00000000-0000-0000-0000-000000000003")
	if err := validateInstallationParams(params); err != nil {
		t.Fatalf("validateInstallationParams(member) = %v, want nil", err)
	}
}
