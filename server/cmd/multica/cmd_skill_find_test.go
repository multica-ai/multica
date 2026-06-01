package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteStructuredAIResultWritesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-task-output.json")
	t.Setenv(aiTaskOutputPathEnv, path)
	payload := `[{"name":"react","description":"React help","source_url":"https://example.test/react","reason":"fits"}]`

	if err := writeStructuredAIResult(payload); err != nil {
		t.Fatalf("writeStructuredAIResult() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != payload {
		t.Fatalf("written payload = %s, want %s", got, payload)
	}
}

func TestWriteStructuredAIResultRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-task-output.json")
	t.Setenv(aiTaskOutputPathEnv, path)

	if err := writeStructuredAIResult(`{"missing"`); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
}

func TestWriteStructuredAIResultRequiresTaskOutputEnv(t *testing.T) {
	old, had := os.LookupEnv(aiTaskOutputPathEnv)
	os.Unsetenv(aiTaskOutputPathEnv)
	t.Cleanup(func() {
		if had {
			os.Setenv(aiTaskOutputPathEnv, old)
		} else {
			os.Unsetenv(aiTaskOutputPathEnv)
		}
	})

	err := writeStructuredAIResult(`[]`)
	if err == nil || !strings.Contains(err.Error(), aiTaskOutputPathEnv) {
		t.Fatalf("expected missing env error, got %v", err)
	}
}

func TestWriteStructuredAIResultRejectsPathTraversal(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "ai-task-output.json"
	t.Setenv(aiTaskOutputPathEnv, path)

	err := writeStructuredAIResult(`[]`)
	if err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("expected path traversal error, got %v", err)
	}
}
