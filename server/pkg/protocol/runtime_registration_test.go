package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeRegistrationCapabilitiesPresence(t *testing.T) {
	empty := []string{}
	omitted, err := json.Marshal(RuntimeRegistration{Name: "legacy", Type: "platform-agent-cli"})
	if err != nil {
		t.Fatalf("marshal omitted registration: %v", err)
	}
	explicit, err := json.Marshal(RuntimeRegistration{Name: "new", Type: "platform-agent-cli", Capabilities: &empty})
	if err != nil {
		t.Fatalf("marshal explicit registration: %v", err)
	}
	if strings.Contains(string(omitted), `"capabilities"`) {
		t.Fatalf("omitted payload = %s", omitted)
	}
	if !strings.Contains(string(explicit), `"capabilities":[]`) {
		t.Fatalf("explicit payload = %s", explicit)
	}

	var decodedOmitted RuntimeRegistration
	if err := json.Unmarshal(omitted, &decodedOmitted); err != nil {
		t.Fatalf("unmarshal omitted registration: %v", err)
	}
	if decodedOmitted.Capabilities != nil {
		t.Fatalf("omitted capabilities = %#v, want nil", decodedOmitted.Capabilities)
	}

	var decodedExplicit RuntimeRegistration
	if err := json.Unmarshal(explicit, &decodedExplicit); err != nil {
		t.Fatalf("unmarshal explicit registration: %v", err)
	}
	if decodedExplicit.Capabilities == nil || len(*decodedExplicit.Capabilities) != 0 {
		t.Fatalf("explicit capabilities = %#v, want non-nil empty slice", decodedExplicit.Capabilities)
	}
}
