package runtimepool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestParseRequirementsAcceptsCanonicalV1(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1","multica.extension.execute/v1"]}`)

	got, err := ParseRequirements(raw)
	if err != nil {
		t.Fatalf("ParseRequirements(%s): %v", raw, err)
	}
	if got.SchemaVersion != "multica.runtime-requirements/v1" {
		t.Fatalf("SchemaVersion = %q, want multica.runtime-requirements/v1", got.SchemaVersion)
	}
	if len(got.CapabilitiesAll) != 2 || got.CapabilitiesAll[0] != "a/v1" || got.CapabilitiesAll[1] != "multica.extension.execute/v1" {
		t.Fatalf("CapabilitiesAll = %#v, want [a/v1 multica.extension.execute/v1]", got.CapabilitiesAll)
	}
}

func TestParseRequirementsRejectsNonCanonicalCapabilities(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unsorted", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["z/v1","a/v1"]}`},
		{"duplicate", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1","a/v1"]}`},
		{"empty", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":[""]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRequirements(json.RawMessage(tc.raw)); err == nil {
				t.Fatalf("ParseRequirements(%s) succeeded; want error", tc.raw)
			}
		})
	}
}

func TestParseRequirementsRejectsInvalidJSONContract(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unknown_field", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"],"future":true}`},
		{"trailing_value", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}{}`},
		{"wrong_schema", `{"schema_version":"multica.runtime-requirements/v2","capabilities_all":["a/v1"]}`},
		{"missing_schema", `{"capabilities_all":["a/v1"]}`},
		{"missing_capabilities", `{"schema_version":"multica.runtime-requirements/v1"}`},
		{"empty_raw", ``},
		{"null", `null`},
		{"scalar", `42`},
		{"schema_wrong_type", `{"schema_version":1,"capabilities_all":["a/v1"]}`},
		{"capabilities_wrong_type", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":"a/v1"}`},
		{"capability_wrong_type", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":[1]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRequirements(json.RawMessage(tc.raw)); err == nil {
				t.Fatalf("ParseRequirements(%s) succeeded; want error", tc.raw)
			}
		})
	}
}

func TestParseRequirementsRejectsDuplicateObjectKeys(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "schema_version",
			raw:  `{"schema_version":"multica.runtime-requirements/v1","schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}`,
		},
		{
			name: "capabilities_all",
			raw:  `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"],"capabilities_all":["a/v1"]}`,
		},
		{
			name: "escaped_equivalent_schema_version",
			raw:  `{"schema_version":"multica.runtime-requirements/v1","schema_\u0076ersion":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseRequirements(json.RawMessage(tc.raw)); err == nil {
				t.Fatalf("ParseRequirements(%s) succeeded; want duplicate-key error", tc.raw)
			}
		})
	}
}

func TestParseRequirementsEnforcesCapabilityCount(t *testing.T) {
	capabilities32 := makeCapabilities(32, 8)
	capabilities33 := makeCapabilities(33, 8)

	if _, err := ParseRequirements(marshalRequirementsForTest(t, capabilities32)); err != nil {
		t.Fatalf("ParseRequirements with 32 capabilities: %v", err)
	}
	if _, err := ParseRequirements(marshalRequirementsForTest(t, capabilities33)); err == nil {
		t.Fatal("ParseRequirements with 33 capabilities succeeded; want error")
	}
}

func TestParseRequirementsEnforcesCapabilityNameContract(t *testing.T) {
	capability128 := "a/" + strings.Repeat("x", 126)
	capability129 := "a/" + strings.Repeat("x", 127)
	cases := []struct {
		name       string
		capability string
		wantError  bool
	}{
		{name: "128_bytes", capability: capability128},
		{name: "129_bytes", capability: capability129, wantError: true},
		{name: "uppercase", capability: "A/v1", wantError: true},
		{name: "leading_slash", capability: "/a/v1", wantError: true},
		{name: "space", capability: "a capability/v1", wantError: true},
		{name: "colon", capability: "a:capability/v1", wantError: true},
		{name: "backslash", capability: `a\capability/v1`, wantError: true},
		{name: "leading_underscore", capability: "_a/v1", wantError: true},
		{name: "leading_dot", capability: ".a/v1", wantError: true},
		{name: "leading_dash", capability: "-a/v1", wantError: true},
		{name: "non_ascii", capability: "能力/v1", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequirements(marshalRequirementsForTest(t, []string{tc.capability}))
			if tc.wantError && err == nil {
				t.Fatalf("ParseRequirements accepted capability %q; want error", tc.capability)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("ParseRequirements rejected capability %q: %v", tc.capability, err)
			}
		})
	}
}

func TestCanonicalRequirementsReturnsStableJSON(t *testing.T) {
	value := Requirements{
		SchemaVersion:   "multica.runtime-requirements/v1",
		CapabilitiesAll: []string{"a/v1", "multica.extension.execute/v1"},
	}

	got, err := CanonicalRequirements(value)
	if err != nil {
		t.Fatalf("CanonicalRequirements: %v", err)
	}
	want := `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1","multica.extension.execute/v1"]}`
	if string(got) != want {
		t.Fatalf("CanonicalRequirements = %s, want %s", got, want)
	}
}

func TestCanonicalRequirementsRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		value Requirements
	}{
		{
			name: "unsorted",
			value: Requirements{
				SchemaVersion:   "multica.runtime-requirements/v1",
				CapabilitiesAll: []string{"z/v1", "a/v1"},
			},
		},
		{
			name: "duplicate",
			value: Requirements{
				SchemaVersion:   "multica.runtime-requirements/v1",
				CapabilitiesAll: []string{"a/v1", "a/v1"},
			},
		},
		{
			name: "invalid_capability",
			value: Requirements{
				SchemaVersion:   "multica.runtime-requirements/v1",
				CapabilitiesAll: []string{"a:capability/v1"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CanonicalRequirements(tc.value); err == nil {
				t.Fatalf("CanonicalRequirements(%#v) succeeded; want error", tc.value)
			}
		})
	}
}

func TestCanonicalRequirementsEnforcesEncodedSize(t *testing.T) {
	// JSON overhead for 31 string entries is 166 bytes. These literal lengths
	// therefore produce canonical payloads of exactly 4096 and 4097 bytes.
	capabilities4096 := makeCapabilities(31, 127)
	capabilities4096[30] = "a30/" + strings.Repeat("x", 116)
	capabilities4097 := append([]string(nil), capabilities4096...)
	capabilities4097[30] += "x"

	got, err := CanonicalRequirements(Requirements{
		SchemaVersion:   "multica.runtime-requirements/v1",
		CapabilitiesAll: capabilities4096,
	})
	if err != nil {
		t.Fatalf("CanonicalRequirements at 4096 bytes: %v", err)
	}
	if len(got) != 4096 {
		t.Fatalf("canonical length = %d, want 4096", len(got))
	}
	if _, err := CanonicalRequirements(Requirements{
		SchemaVersion:   "multica.runtime-requirements/v1",
		CapabilitiesAll: capabilities4097,
	}); err == nil {
		t.Fatal("CanonicalRequirements at 4097 bytes succeeded; want error")
	}
}

func TestParseRequirementsRejectsCanonicalPayloadOver4096Bytes(t *testing.T) {
	capabilities4097 := makeCapabilities(31, 127)
	capabilities4097[30] = "a30/" + strings.Repeat("x", 117)
	raw := marshalRequirementsForTest(t, capabilities4097)
	if len(raw) != 4097 {
		t.Fatalf("test payload length = %d, want 4097", len(raw))
	}
	if _, err := ParseRequirements(raw); err == nil {
		t.Fatal("ParseRequirements at 4097 bytes succeeded; want error")
	}
}

func makeCapabilities(count, length int) []string {
	capabilities := make([]string, count)
	for i := range capabilities {
		prefix := fmt.Sprintf("a%02d/", i)
		capabilities[i] = prefix + strings.Repeat("x", length-len(prefix))
	}
	return capabilities
}

func marshalRequirementsForTest(t *testing.T, capabilities []string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(struct {
		SchemaVersion   string   `json:"schema_version"`
		CapabilitiesAll []string `json:"capabilities_all"`
	}{
		SchemaVersion:   "multica.runtime-requirements/v1",
		CapabilitiesAll: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
