package toolpolicy

import "testing"

// Effective.Openable is a display hint (FIR-3091) the admin table reads to tell
// an openable workspace Deny apart from an unopenable floor, so the frontend
// never re-derives the resolution mode. It must be true for every openable
// (member-override) resolution and false for every tighten-only one.
func TestEffective_Openable(t *testing.T) {
	t.Run("openable resolution sets Openable=true", func(t *testing.T) {
		cases := []Input{
			{Settings: set(LayerWorkspace, SettingDeny)},                           // openable workspace Deny
			{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow)}, // Group opens it
			{Settings: set(LayerUser, SettingAsk), IsSystem: true},                 // system-ask fail-safe branch
			{Settings: set(LayerWorkspace, SettingDisable)},                        // disabled floor is still openable-mode
		}
		for i, in := range cases {
			if got := ResolveWithMode(ModeOpenable, in); !got.Openable {
				t.Fatalf("case %d: ResolveWithMode(ModeOpenable).Openable = false, want true (%+v)", i, got)
			}
		}
	})

	t.Run("hard-floor resolution sets Openable=false", func(t *testing.T) {
		cases := []Input{
			{Settings: set(LayerWorkspace, SettingDeny)},
			{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow)},
			{Settings: set(LayerUser, SettingAsk), IsSystem: true},
			{Base: SettingDeny},
		}
		for i, in := range cases {
			if got := ResolveWithMode(ModeHardFloor, in); got.Openable {
				t.Fatalf("case %d: ResolveWithMode(ModeHardFloor).Openable = true, want false (%+v)", i, got)
			}
			// Resolve is the tighten-only wrapper — also never openable.
			if got := Resolve(in); got.Openable {
				t.Fatalf("case %d: Resolve().Openable = true, want false (%+v)", i, got)
			}
		}
	})
}
