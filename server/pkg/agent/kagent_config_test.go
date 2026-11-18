package agent

import "testing"

func TestKagentRuntimeConfig(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"mode":"coding"}`, `{"mode":"native-text","endpoint":"https://other"}`, `{"mode":"native-text"} {}`} {
		if err := ValidateKagentRuntimeConfig([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := ValidateKagentRuntimeConfig([]byte(`{"mode":"native-text"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveBackend("kagent", Config{Env: map[string]string{"MULTICA_KAGENT_ENABLED": "1"}}); err == nil {
		t.Fatal("agent env enabled remote backend")
	}
	if IsSupportedType("kagent") {
		t.Fatal("remote backend became a custom CLI protocol")
	}
}
