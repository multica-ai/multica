package connections

import (
	"encoding/json"
	"testing"
)

// TestParseOptions_DataSourcesListShape covers the real registry payload: an MCP
// tools/call result whose text content carries {count, data_sources:[...]} with
// folder_name + publish_tags (FIR-2083 Phase 1 normalized shape).
func TestParseOptions_DataSourcesListShape(t *testing.T) {
	inner := map[string]any{
		"count": 2,
		"data_sources": []any{
			map[string]any{"id": "ds-1", "name": "Orders", "folder_name": "Finance", "publish_tags": []any{"core", "pii"}},
			map[string]any{"id": "ds-2", "name": "Refunds", "folder_name": "Finance", "publish_tags": []any{}},
		},
	}
	innerJSON, _ := json.Marshal(inner)
	result, _ := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(innerJSON)}},
	})

	opts := parseOptions(result)
	if len(opts) != 2 {
		t.Fatalf("want 2 options, got %d (%+v)", len(opts), opts)
	}
	if opts[0].ID != "ds-1" || opts[0].Name != "Orders" || opts[0].Folder != "Finance" {
		t.Fatalf("option 0 mismatch: %+v", opts[0])
	}
	if len(opts[0].Tags) != 2 || opts[0].Tags[0] != "core" {
		t.Fatalf("option 0 tags mismatch: %+v", opts[0].Tags)
	}
	if opts[1].ID != "ds-2" || opts[1].Folder != "Finance" {
		t.Fatalf("option 1 mismatch: %+v", opts[1])
	}
}

// TestParseOptions_StructuredContent prefers structuredContent when present.
func TestParseOptions_StructuredContent(t *testing.T) {
	result, _ := json.Marshal(map[string]any{
		"structuredContent": map[string]any{
			"data_sources": []any{map[string]any{"id": "x", "name": "X", "folder": "F"}},
		},
		"content": []any{map[string]any{"type": "text", "text": "ignored garbage"}},
	})
	opts := parseOptions(result)
	if len(opts) != 1 || opts[0].ID != "x" || opts[0].Folder != "F" {
		t.Fatalf("structuredContent not preferred: %+v", opts)
	}
}

// TestParseOptions_BareArray handles a tool that returns a JSON array directly.
func TestParseOptions_BareArray(t *testing.T) {
	arr, _ := json.Marshal([]any{
		map[string]any{"id": "a", "name": "A"},
		map[string]any{"id": "b"}, // no name → falls back to id
	})
	result, _ := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(arr)}},
	})
	opts := parseOptions(result)
	if len(opts) != 2 {
		t.Fatalf("want 2, got %d", len(opts))
	}
	if opts[1].ID != "b" || opts[1].Name != "b" {
		t.Fatalf("missing name should fall back to id: %+v", opts[1])
	}
}

// TestParseOptions_SkipsRecordsWithoutID drops unusable records.
func TestParseOptions_SkipsRecordsWithoutID(t *testing.T) {
	arr, _ := json.Marshal([]any{
		map[string]any{"name": "no id"},
		map[string]any{"id": "ok", "name": "OK"},
	})
	result, _ := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(arr)}},
	})
	opts := parseOptions(result)
	if len(opts) != 1 || opts[0].ID != "ok" {
		t.Fatalf("want only the record with an id: %+v", opts)
	}
}

// TestMarshalScopableArgs_DropsInertEntries: an entry missing tool/arg/source is
// meaningless and must never be persisted; valid entries are trimmed and kept.
func TestMarshalScopableArgs_DropsInertEntries(t *testing.T) {
	in := []ScopableArg{
		{Tool: " query_run ", Arg: " data_source_id ", OptionsSourceTool: " data_sources_list ", GroupBy: "folder"},
		{Tool: "query_run", Arg: "", OptionsSourceTool: "data_sources_list"}, // inert: no arg
		{Tool: "", Arg: "x", OptionsSourceTool: "y"},                         // inert: no tool
	}
	b, err := marshalScopableArgs(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []ScopableArg
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 surviving entry, got %d (%+v)", len(out), out)
	}
	if out[0].Tool != "query_run" || out[0].Arg != "data_source_id" || out[0].OptionsSourceTool != "data_sources_list" {
		t.Fatalf("entry not trimmed correctly: %+v", out[0])
	}
}

// TestMarshalScopableArgs_NilIsEmptyArray keeps the column behavior-preserving.
func TestMarshalScopableArgs_NilIsEmptyArray(t *testing.T) {
	b, err := marshalScopableArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Fatalf("nil should marshal to []: got %s", b)
	}
}

// TestOptionsSourceFor gates the options endpoint to declared tools only.
func TestOptionsSourceFor(t *testing.T) {
	c := Connection{ScopableArgs: []ScopableArg{
		{Tool: "query_run", Arg: "data_source_id", OptionsSourceTool: "data_sources_list"},
	}}
	if _, ok := c.OptionsSourceFor("data_sources_list"); !ok {
		t.Fatal("declared options source should be allowed")
	}
	if _, ok := c.OptionsSourceFor("query_run"); ok {
		t.Fatal("a non-options tool must not be drivable through the options endpoint")
	}
	if _, ok := c.OptionsSourceFor("arbitrary_tool"); ok {
		t.Fatal("an undeclared tool must be rejected")
	}
}

func TestTrimCredentials(t *testing.T) {
	got := TrimCredentials(AuthConfig{
		BearerToken:    " rk_abc\n",
		APIKey:         "\tkey ",
		APIKeyHeader:   " X-API-Key ",
		CFAccessID:     " id ",
		CFAccessSecret: " secret\r\n",
	})
	if got.BearerToken != "rk_abc" || got.APIKey != "key" || got.APIKeyHeader != "X-API-Key" ||
		got.CFAccessID != "id" || got.CFAccessSecret != "secret" {
		t.Fatalf("credentials not trimmed: %+v", got)
	}
}
