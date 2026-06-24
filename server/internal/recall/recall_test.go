package recall

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsRankedBoundedHitsWithProvenance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 24, 9, 0, 0, 0, time.UTC)
	vault := t.TempDir()
	writeTestNote(t, vault, "notes/runtime recall is bounded.md", "Runtime recall uses a strict byte budget and at most five hits.")
	writeTestNote(t, vault, "notes/unrelated cooking note.md", "Pasta needs salted water.")
	writeTestIndex(t, vault, now, []map[string]any{
		{
			"path": "notes/unrelated cooking note.md", "title": "unrelated cooking note",
			"tags": []string{"cooking"}, "mtime": now.Add(-time.Hour).Format(time.RFC3339),
			"summary": "pasta recipes", "folder_class": "notes", "size_bytes": 24,
		},
		{
			"path": "notes/runtime recall is bounded.md", "title": "runtime recall is bounded",
			"tags": []string{"runtime", "memory"}, "mtime": now.Add(-2 * time.Hour).Format(time.RFC3339),
			"summary": "bounded shared memory recall for agent runs", "folder_class": "notes", "size_bytes": 72,
		},
	})

	result := Run(context.Background(), Options{
		VaultRoot:      vault,
		MaxHits:        5,
		MaxBundleBytes: 900,
		MaxIndexAge:    24 * time.Hour,
		Now:            func() time.Time { return now },
	}, Query{
		IssueTitle:       "Implement bounded shared memory recall",
		IssueDescription: "Agent runs need deterministic retrieval",
		TriggerComment:   "Start with the runtime recall implementation",
	})

	if result.Status != StatusHit {
		t.Fatalf("status = %q, want %q (reason=%q)", result.Status, StatusHit, result.Reason)
	}
	if result.HitCount != 1 || len(result.Hits) != 1 {
		t.Fatalf("hits = %d/%d, want exactly one relevant hit", result.HitCount, len(result.Hits))
	}
	hit := result.Hits[0]
	if hit.Path != "notes/runtime recall is bounded.md" {
		t.Fatalf("first hit path = %q", hit.Path)
	}
	if hit.Recency != now.Add(-2*time.Hour).Format(time.RFC3339) {
		t.Fatalf("recency = %q", hit.Recency)
	}
	if hit.Relevance <= 0 {
		t.Fatalf("relevance = %f, want positive", hit.Relevance)
	}
	if hit.Excerpt == "" {
		t.Fatal("excerpt is empty")
	}
	rendered := result.Render()
	if len([]byte(rendered)) > 900 {
		t.Fatalf("rendered bundle uses %d bytes, budget is 900", len([]byte(rendered)))
	}
	if result.BytesUsed != len([]byte(rendered)) {
		t.Fatalf("bytes_used = %d, rendered bytes = %d", result.BytesUsed, len([]byte(rendered)))
	}
	for _, required := range []string{"recall_status", "index_version", "path", "recency", "relevance"} {
		if !strings.Contains(rendered, required) {
			t.Errorf("rendered bundle missing %q: %s", required, rendered)
		}
	}
}

func TestRunCapsHitCountAndExcludesUnsafeSources(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 24, 9, 0, 0, 0, time.UTC)
	vault := t.TempDir()
	entries := make([]map[string]any, 0, 10)
	for i := 0; i < 7; i++ {
		path := filepath.ToSlash(filepath.Join("notes", "memory recall "+string(rune('a'+i))+".md"))
		writeTestNote(t, vault, path, "shared memory recall content")
		entries = append(entries, map[string]any{
			"path": path, "title": "shared memory recall", "tags": []string{"memory"},
			"mtime": now.Format(time.RFC3339), "summary": "shared memory recall", "folder_class": "notes", "size_bytes": 28,
		})
	}
	writeTestNote(t, vault, "notes/MEMORY.md", "must never be global")
	writeTestNote(t, vault, "notes/agent-memory-index.md", "aggregated agent-local MEMORY.md content")
	writeTestNote(t, vault, "self/private memory.md", "must never be global")
	entries = append(entries,
		map[string]any{"path": "notes/MEMORY.md", "title": "shared memory", "mtime": now.Format(time.RFC3339), "summary": "memory", "folder_class": "notes", "size_bytes": 20},
		map[string]any{"path": "notes/agent-memory-index.md", "title": "shared agent memory", "mtime": now.Format(time.RFC3339), "summary": "memory", "folder_class": "notes", "size_bytes": 20},
		map[string]any{"path": "self/private memory.md", "title": "shared memory", "mtime": now.Format(time.RFC3339), "summary": "memory", "folder_class": "self", "size_bytes": 20},
		map[string]any{"path": "../outside.md", "title": "shared memory", "mtime": now.Format(time.RFC3339), "summary": "memory", "folder_class": "notes", "size_bytes": 20},
	)
	writeTestIndex(t, vault, now, entries)

	result := Run(context.Background(), Options{
		VaultRoot: vault, MaxHits: 3, MaxBundleBytes: 4096, MaxIndexAge: time.Hour,
		Now: func() time.Time { return now },
	}, Query{IssueTitle: "shared memory recall"})

	if len(result.Hits) != 3 {
		t.Fatalf("hit count = %d, want configured cap 3", len(result.Hits))
	}
	for _, hit := range result.Hits {
		if hit.Path == "notes/MEMORY.md" || hit.Path == "notes/agent-memory-index.md" || strings.HasPrefix(hit.Path, "self/") || strings.Contains(hit.Path, "..") {
			t.Fatalf("unsafe source included: %q", hit.Path)
		}
	}
}

func TestRunReturnsExplicitNonBlockingStatuses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 24, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		prepare    func(t *testing.T, vault string)
		wantStatus Status
		wantReason string
	}{
		{name: "unconfigured", wantStatus: StatusNoHit, wantReason: "vault_not_configured"},
		{name: "missing index", prepare: func(t *testing.T, vault string) {}, wantStatus: StatusNoHit, wantReason: "index_missing"},
		{name: "malformed index", prepare: func(t *testing.T, vault string) {
			writeTestFile(t, filepath.Join(vault, "ops", "recall-index.json"), "not-json")
		}, wantStatus: StatusControlledError, wantReason: "index_invalid"},
		{name: "stale index", prepare: func(t *testing.T, vault string) {
			writeTestIndex(t, vault, now.Add(-48*time.Hour), nil)
		}, wantStatus: StatusNoHit, wantReason: "index_stale"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vault := ""
			if tc.prepare != nil {
				vault = t.TempDir()
				tc.prepare(t, vault)
			}
			result := Run(context.Background(), Options{
				VaultRoot: vault, MaxHits: 5, MaxBundleBytes: 2048, MaxIndexAge: 24 * time.Hour,
				Now: func() time.Time { return now },
			}, Query{IssueTitle: "memory"})
			if result.Status != tc.wantStatus || result.Reason != tc.wantReason {
				t.Fatalf("status/reason = %q/%q, want %q/%q", result.Status, result.Reason, tc.wantStatus, tc.wantReason)
			}
			if result.Render() == "" {
				t.Fatal("status bundle must always be visible")
			}
		})
	}
}

func TestRunBoundsLongQueryEvenWithoutHits(t *testing.T) {
	t.Parallel()

	result := Run(context.Background(), Options{
		MaxBundleBytes: 512,
	}, Query{IssueDescription: strings.Repeat("very long issue context ", 1000)})
	rendered := result.Render()
	if len([]byte(rendered)) > 512 {
		t.Fatalf("no-hit bundle uses %d bytes, budget is 512", len([]byte(rendered)))
	}
	if result.Status != StatusNoHit {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestBuildIndexUsesCanonicalNotesSchema(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 24, 9, 0, 0, 0, time.UTC)
	vault := t.TempDir()
	writeTestNote(t, vault, "notes/shared recall stays bounded.md", "---\ndescription: Recall has deterministic limits\ntype: decision\ncreated: 2026-06-24\n---\n\n# Shared recall stays bounded\n\nBody.\n\n---\n\nTopics:\n- [[agent memory]]\n")
	writeTestNote(t, vault, "notes/MEMORY.md", "excluded")
	writeTestNote(t, vault, "notes/agent-memory-index.md", "excluded")
	writeTestNote(t, vault, "notes/Unbenannt 1.md", "excluded")
	writeTestNote(t, vault, "self/identity.md", "excluded")
	writeTestNote(t, vault, "daily/2026-06-24.md", "excluded")
	writeTestNote(t, vault, "notes/stale.sync-conflict-20260624.md", "excluded")

	index, err := BuildIndex(context.Background(), IndexOptions{
		VaultRoot: vault,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}
	if index.IndexVersion != CurrentIndexVersion || index.EntryCount != 1 || len(index.Entries) != 1 {
		t.Fatalf("index version/count = %d/%d entries=%d", index.IndexVersion, index.EntryCount, len(index.Entries))
	}
	entry := index.Entries[0]
	if entry.Path != "notes/shared recall stays bounded.md" {
		t.Fatalf("entry path = %q", entry.Path)
	}
	if entry.Summary != "Recall has deterministic limits" {
		t.Fatalf("entry summary = %q", entry.Summary)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "agent memory" {
		t.Fatalf("entry tags = %#v", entry.Tags)
	}

	data, err := os.ReadFile(filepath.Join(vault, "ops", "recall-index.json"))
	if err != nil {
		t.Fatalf("read generated index: %v", err)
	}
	var persisted Index
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("generated index is invalid JSON: %v", err)
	}
	if persisted.EntryCount != 1 {
		t.Fatalf("persisted entry_count = %d", persisted.EntryCount)
	}
	matches, err := filepath.Glob(filepath.Join(vault, "ops", ".recall-index-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary index files left behind: %#v", matches)
	}
}

func writeTestIndex(t *testing.T, vault string, generatedAt time.Time, entries []map[string]any) {
	t.Helper()
	payload := map[string]any{
		"index_version": CurrentIndexVersion,
		"generated_at":  generatedAt.Format(time.RFC3339),
		"vault_commit":  "test-commit",
		"entry_count":   len(entries),
		"entries":       entries,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(vault, "ops", "recall-index.json"), string(data))
}

func writeTestNote(t *testing.T, vault, relativePath, content string) {
	t.Helper()
	writeTestFile(t, filepath.Join(vault, filepath.FromSlash(relativePath)), content)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
