package handler

import (
	"encoding/json"
	"testing"
)

// VWO-367: validateLocalDirectoryRef re-marshals the payload through the typed
// struct, so any field not on localDirectoryRef is silently dropped. These
// cases pin the isolate flag's round-trip so a future struct edit can't
// silently strip the opt-in (which would flip an isolated fleet back to
// unserialized in-place writes on its next task). Pure unit test — no DB.
func TestValidateLocalDirectoryRefIsolateRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("isolate true survives validation", func(t *testing.T) {
		out, err := validateLocalDirectoryRef(json.RawMessage(`{"local_path":"/Users/u/proj","daemon_id":"d1","isolate":true}`))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(out, &ref); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !ref.Isolate {
			t.Fatal("isolate=true was dropped by the validation re-marshal")
		}
	})

	t.Run("isolate defaults false and stays omitted", func(t *testing.T) {
		out, err := validateLocalDirectoryRef(json.RawMessage(`{"local_path":"/Users/u/proj","daemon_id":"d1"}`))
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(out, &ref); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ref.Isolate {
			t.Fatal("isolate defaulted true")
		}
		// omitempty: the default payload shape is unchanged for existing rows.
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("unmarshal map: %v", err)
		}
		if _, present := m["isolate"]; present {
			t.Fatal("isolate=false should be omitted from the stored payload")
		}
	})
}
