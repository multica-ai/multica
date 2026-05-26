package sandboxprofile

import (
	"encoding/json"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty bytes", "", ProfileDeveloper},
		{"empty object", "{}", ProfileDeveloper},
		{"null", "null", ProfileDeveloper},
		{"explicit zero fields is developer", `{"deny_shell":false}`, ProfileDeveloper},
		{"deny_shell only is read_only", `{"deny_shell":true}`, ProfileReadOnly},
		{"deny_shell with whitespace", `  {"deny_shell": true}  `, ProfileReadOnly},
		{"deny_shell plus extra is custom", `{"deny_shell":true,"denied_paths":["/etc"]}`, ProfileCustom},
		{"custom network mode", `{"network_mode":"custom","allowed_hosts":["api.example.com:443"]}`, ProfileCustom},
		{"unparseable is custom", `["not","a","policy"]`, ProfileCustom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("Classify(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPolicyJSONForProfile(t *testing.T) {
	// Selectable presets expand to JSON that round-trips back to the same profile.
	for _, id := range []string{ProfileDeveloper, ProfileReadOnly} {
		raw, ok := PolicyJSONForProfile(id)
		if !ok {
			t.Fatalf("PolicyJSONForProfile(%q) returned ok=false", id)
		}
		if got := Classify(raw); got != id {
			t.Fatalf("round-trip: expanded %q then classified as %q", id, got)
		}
	}

	// Custom / unknown ids are not expandable — the caller must reject them.
	for _, id := range []string{ProfileCustom, "", "bogus"} {
		if _, ok := PolicyJSONForProfile(id); ok {
			t.Fatalf("PolicyJSONForProfile(%q) unexpectedly ok=true", id)
		}
	}
}

func TestDefinitionsShape(t *testing.T) {
	defs := Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 profile definitions, got %d", len(defs))
	}
	// Exactly one non-selectable profile (custom), and every selectable id expands.
	selectable := 0
	for _, d := range defs {
		if d.Label == "" || d.Description == "" {
			t.Fatalf("profile %q missing label/description", d.ID)
		}
		if d.Selectable {
			selectable++
			if _, ok := PolicyJSONForProfile(d.ID); !ok {
				t.Fatalf("selectable profile %q does not expand to a policy", d.ID)
			}
		}
	}
	if selectable != 2 {
		t.Fatalf("expected 2 selectable profiles, got %d", selectable)
	}
}
