package roles

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestMarshalRolePermissionsKeepsScopedRules(t *testing.T) {
	raw, err := marshalRolePermissions(RolePermissions{
		"connection:company-brain": {
			{Setting: "allow", ResourcePattern: "search"},
			{Setting: "deny", ResourcePattern: "write"},
		},
	})
	if err != nil {
		t.Fatalf("marshalRolePermissions: %v", err)
	}
	var got RolePermissions
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got["connection:company-brain"]) != 2 {
		t.Fatalf("rules = %#v, want both resource-scoped decisions", got)
	}
}

func TestMarshalRolePermissionsRejectsUnknownSetting(t *testing.T) {
	_, err := marshalRolePermissions(RolePermissions{
		"create_issue": {{Setting: "future_value"}},
	})
	if !errors.Is(err, ErrInvalidPermissions) {
		t.Fatalf("error = %v, want ErrInvalidPermissions", err)
	}
}

func TestMarshalRolePermissionsRejectsDuplicateScope(t *testing.T) {
	_, err := marshalRolePermissions(RolePermissions{
		"create_issue": {
			{Setting: "allow", ResourcePattern: ""},
			{Setting: "deny", ResourcePattern: ""},
		},
	})
	if !errors.Is(err, ErrInvalidPermissions) {
		t.Fatalf("error = %v, want ErrInvalidPermissions", err)
	}
}
