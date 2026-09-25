package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A plugin's skill resource becomes an ordinary workspace skill, and uninstall
// takes it away again.
//
// The whole design claim is that this needs no new machinery: the skill table
// already holds a SKILL.md, so the only addition is remembering who contributed
// it. These tests are about that ownership — that it installs, that an upgrade
// prunes what the manifest dropped, that uninstall removes exactly the
// plugin's rows, and that a plugin cannot overwrite a skill a person wrote.

const skillPluginManifest = `{
  "manifest_version": 1,
  "key": "com.example.skilled",
  "name": "Skilled",
  "version": "1.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "resources": [{ "type": "skill", "key": "pr-review", "entry": "skills/pr-review/SKILL.md" }]
  }
}`

const prReviewSkill = `---
name: pr-review
description: Review a pull request against the repository's conventions.
---

Read the diff, then check it against CLAUDE.md.
`

func writePluginSkillFile(t *testing.T, root, key, content string) {
	t.Helper()
	dir := filepath.Join(root, "hello", "skills", key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func installSkillPlugin(t *testing.T, root, manifest string) string {
	t.Helper()
	versionID := withLocalPluginSourceIn(t, root, manifest)
	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"issues:read"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func skillNamesForInstallation(t *testing.T, installationID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT name FROM skill WHERE plugin_installation_id = $1 ORDER BY name`, installationID)
	if err != nil {
		t.Fatalf("query plugin skills: %v", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	return names
}

func TestPluginSkillInstallsAndIsRemovedOnUninstall(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	root := t.TempDir()
	writePluginSkillFile(t, root, "pr-review", prReviewSkill)
	installationID := installSkillPlugin(t, root, skillPluginManifest)

	names := skillNamesForInstallation(t, installationID)
	if len(names) != 1 || names[0] != "pr-review" {
		t.Fatalf("installed skills = %v, want [pr-review]", names)
	}

	// The content is the file, not a stub: a skill that installs empty is worse
	// than one that fails, because the agent gets a plausible no-op.
	var content, description string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content, description FROM skill WHERE plugin_installation_id = $1`, installationID,
	).Scan(&content, &description); err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if len(content) < 20 {
		t.Fatalf("skill content looks empty: %q", content)
	}
	if description == "" {
		t.Fatal("description should come from the frontmatter")
	}

	uninstall := httptest.NewRecorder()
	testHandler.UninstallPlugin(uninstall, pluginHandlerRequest(http.MethodDelete, "/plugins", nil, map[string]string{
		"id": testWorkspaceID, "installationId": installationID,
	}))
	if uninstall.Code != http.StatusNoContent {
		t.Fatalf("uninstall: status=%d body=%s", uninstall.Code, uninstall.Body.String())
	}
	if remaining := skillNamesForInstallation(t, installationID); len(remaining) != 0 {
		t.Fatalf("uninstall left skills behind: %v", remaining)
	}
}

// An upgrade that drops a skill must remove it. Otherwise a renamed skill
// leaves its predecessor in the workspace forever, with nothing to attribute
// it to.
func TestPluginSkillUpgradePrunesWhatTheManifestDropped(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	root := t.TempDir()
	writePluginSkillFile(t, root, "pr-review", prReviewSkill)
	installationID := installSkillPlugin(t, root, skillPluginManifest)

	// Version 2 renames the skill.
	writePluginSkillFile(t, root, "code-review", prReviewSkill)
	upgraded := `{
  "manifest_version": 1,
  "key": "com.example.skilled",
  "name": "Skilled",
  "version": "2.0.0",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "resources": [{ "type": "skill", "key": "code-review", "entry": "skills/code-review/SKILL.md" }]
  }
}`
	if got := installSkillPlugin(t, root, upgraded); got != installationID {
		t.Fatalf("upgrade created a second installation: %s != %s", got, installationID)
	}

	names := skillNamesForInstallation(t, installationID)
	if len(names) != 1 || names[0] != "code-review" {
		t.Fatalf("after upgrade skills = %v, want [code-review] — the dropped one was not pruned", names)
	}
}

// A plugin must not be able to take over a name a person already used. The
// upsert is scoped to rows this installation owns, so the collision surfaces
// as a failed install rather than a silently replaced skill.
func TestPluginSkillWillNotOverwriteAHumanAuthoredSkill(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)

	var existingID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO skill (workspace_id, name, description, content) VALUES ($1, 'pr-review', 'mine', 'written by a person') RETURNING id`,
		testWorkspaceID,
	).Scan(&existingID); err != nil {
		t.Fatalf("seed human skill: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, existingID)
	})

	root := t.TempDir()
	writePluginSkillFile(t, root, "pr-review", prReviewSkill)
	versionID := withLocalPluginSourceIn(t, root, skillPluginManifest)
	body, _ := json.Marshal(map[string]any{
		"version_id":     versionID,
		"granted_scopes": []string{"issues:read"},
	})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code == http.StatusCreated {
		t.Fatal("a plugin claiming an existing skill name must not install")
	}

	// And the person's skill is untouched.
	var content string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill WHERE id = $1`, existingID).Scan(&content); err != nil {
		t.Fatalf("re-read human skill: %v", err)
	}
	if content != "written by a person" {
		t.Fatalf("the human-authored skill was overwritten: %q", content)
	}
}
