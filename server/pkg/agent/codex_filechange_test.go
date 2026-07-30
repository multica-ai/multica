package agent

import "testing"

// Payloads captured from codex-cli 0.145.0 driving
// `app-server --listen stdio://`, so these fixtures document the real schema
// rather than an assumed one. item/started and item/completed carry the same
// `changes` array; only `status` differs.

func fileChangeItem(status string) map[string]any {
	return map[string]any{
		"type":   "fileChange",
		"id":     "call_vxAMM6SbYYIxsWptB3Jmk3JW",
		"status": status,
		"changes": []any{
			map[string]any{
				"path": "/tmp/probe/sample.py",
				"kind": map[string]any{"type": "update", "move_path": nil},
				"diff": "@@ -1,2 +1,2 @@\n def greet(name):\n-    return f\"Hello, {name}!\"\n+    return f\"Hey, {name}!\"\n",
			},
		},
	}
}

func TestCodexFileChangeInputCarriesPathKindAndDiff(t *testing.T) {
	t.Parallel()

	got := codexFileChangeInput(fileChangeItem("inProgress"))
	changes, ok := got["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("expected one change, got %#v", got)
	}
	change, ok := changes[0].(map[string]any)
	if !ok {
		t.Fatalf("expected a change object, got %#v", changes[0])
	}
	if change["path"] != "/tmp/probe/sample.py" {
		t.Errorf("path = %v", change["path"])
	}
	if change["kind"] != "update" {
		t.Errorf("kind should flatten to its discriminator, got %v", change["kind"])
	}
	if _, present := change["move_path"]; present {
		t.Errorf("a null move_path must not be carried through: %#v", change)
	}
	diff, _ := change["diff"].(string)
	if diff == "" || diff[:2] != "@@" {
		t.Errorf("diff should be the unified diff verbatim, got %q", diff)
	}
}

func TestCodexFileChangeInputKeepsRenameTarget(t *testing.T) {
	t.Parallel()

	item := map[string]any{"changes": []any{map[string]any{
		"path": "/tmp/old.py",
		"kind": map[string]any{"type": "update", "move_path": "/tmp/new.py"},
		"diff": "@@ -1 +1 @@\n-a\n+b\n",
	}}}
	changes, _ := codexFileChangeInput(item)["changes"].([]any)
	change, _ := changes[0].(map[string]any)
	if change["move_path"] != "/tmp/new.py" {
		t.Errorf("a rename must keep its target, got %#v", change)
	}
}

func TestCodexFileChangeInputIsNilWhenThereIsNothingToRecord(t *testing.T) {
	t.Parallel()

	// An absent, empty, or unusably-shaped `changes` must produce no payload
	// rather than an empty object: the UI treats an empty input as "no detail"
	// and would otherwise offer an expander with nothing behind it.
	for name, item := range map[string]map[string]any{
		"absent":     {"type": "fileChange"},
		"empty":      {"changes": []any{}},
		"wrong type": {"changes": "not-an-array"},
		"junk entry": {"changes": []any{"not-an-object"}},
		"no fields":  {"changes": []any{map[string]any{"unrelated": 1}}},
	} {
		if got := codexFileChangeInput(item); got != nil {
			t.Errorf("%s: expected nil, got %#v", name, got)
		}
	}
}

func TestCodexFileChangeSummaryReportsStatusAndPaths(t *testing.T) {
	t.Parallel()

	if got := codexFileChangeSummary(fileChangeItem("completed")); got != "completed: /tmp/probe/sample.py" {
		t.Errorf("got %q", got)
	}

	multi := map[string]any{"status": "completed", "changes": []any{
		map[string]any{"path": "/a.py"},
		map[string]any{"path": "/b.py"},
	}}
	if got := codexFileChangeSummary(multi); got != "completed: /a.py, /b.py" {
		t.Errorf("multi-file summary = %q", got)
	}

	// A missing status still reads as a result rather than an empty string,
	// which the transcript renders as "no output".
	if got := codexFileChangeSummary(map[string]any{"changes": []any{}}); got != "completed" {
		t.Errorf("fallback status = %q", got)
	}
}
