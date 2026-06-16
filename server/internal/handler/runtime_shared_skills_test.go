package handler

import (
	"testing"
)

func TestValidateSharedSkillFilesRejectsInvalidPaths(t *testing.T) {
	_, err := validateSharedSkillFiles([]CreateSkillFileRequest{
		{Path: "../escape.txt", Content: "nope"},
		{Path: "ok.txt", Content: "fine"},
	})
	if err == nil {
		t.Fatal("expected invalid path error")
	}
}

func TestValidateSharedSkillFilesAcceptsValidPaths(t *testing.T) {
	files, err := validateSharedSkillFiles([]CreateSkillFileRequest{
		{Path: "scripts/run.sh", Content: "#!/bin/sh"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestHashRuntimeSharedSkillStableAcrossFileOrder(t *testing.T) {
	a := hashRuntimeSharedSkill("content", []CreateSkillFileRequest{
		{Path: "b.txt", Content: "2"},
		{Path: "a.txt", Content: "1"},
	})
	b := hashRuntimeSharedSkill("content", []CreateSkillFileRequest{
		{Path: "a.txt", Content: "1"},
		{Path: "b.txt", Content: "2"},
	})
	if a != b {
		t.Fatalf("hash mismatch: %s vs %s", a, b)
	}
}

func TestRuntimeSharedSkillOriginParsesSyncKey(t *testing.T) {
	raw := []byte(`{"origin":{"type":"runtime_shared","runtime_id":"rt-1","sync_key":"find-skills","content_hash":"sha256:abc"}}`)
	origin := runtimeSharedSkillOrigin(raw)
	if origin == nil {
		t.Fatal("expected origin")
	}
	if origin.SyncKey != "find-skills" || origin.RuntimeID != "rt-1" {
		t.Fatalf("unexpected origin: %+v", origin)
	}
}
