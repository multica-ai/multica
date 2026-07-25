package toolpolicy

import "testing"

func TestDeclaredSafetyFloorsCannotBeOpenedBySpecificAllow(t *testing.T) {
	in := Input{
		Base: SettingAllow,
		Settings: map[Layer]Setting{
			LayerWorkspace: SettingDeny,
			LayerAgent:     SettingAllow,
			LayerUser:      SettingAllow,
		},
	}

	for _, key := range []string{
		"credential.reveal",
		"tools:agent-browser",
		"repo.checkout",
		"repo.push",
		RegistryToolKey,
	} {
		t.Run(key, func(t *testing.T) {
			mode := DeclaredResolutionMode(key, ModeOpenable)
			got := ResolveWithMode(mode, in)
			if mode != ModeHardFloor || got.Setting != SettingDeny {
				t.Fatalf("%s resolved with mode=%q setting=%q, want hard-floor deny", key, mode, got.Setting)
			}
		})
	}
}

func TestDeclaredOrdinaryPermissionKeepsWorkspaceOpenableMode(t *testing.T) {
	in := Input{
		Base: SettingAllow,
		Settings: map[Layer]Setting{
			LayerWorkspace: SettingDeny,
			LayerUser:      SettingAllow,
		},
	}
	mode := DeclaredResolutionMode("add_comment", ModeOpenable)
	got := ResolveWithMode(mode, in)
	if mode != ModeOpenable || got.Setting != SettingAllow {
		t.Fatalf("ordinary permission resolved with mode=%q setting=%q, want openable allow", mode, got.Setting)
	}
}
