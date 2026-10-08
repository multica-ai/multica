package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Publish, install and delete race each other.
//
// Relationships are application-owned by repository policy, so there is no
// foreign key making "the version this installation names still exists" true.
// The interleaving that matters is `delete counts zero installs` → `install
// reads the version` → `delete commits` → `install commits`: the installation
// survives pointing at a version that is gone, its panel 404s forever, and
// nothing in the product can explain why. A (workspace, plugin key) advisory
// lock held for each transaction is what serializes them.
//
// These run the real handlers against the real database, because the bug only
// exists at the transaction boundary — a test with a fake would assert the
// structure of the fix rather than its effect.

// TestDeleteAndInstallRaceLeavesNoDanglingInstallation is the direct regression.
// Whichever side wins, the end state has to be self-consistent: either the
// package is gone and nothing installed it, or the installation exists and its
// version is still there.
func TestDeleteAndInstallRaceLeavesNoDanglingInstallation(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	withHostCapabilities(t)

	// Repeated because a lost race is a scheduling accident: a single pass can
	// miss the window even when the lock is absent.
	for attempt := range 12 {
		cleanupPluginInstallations(t)
		published := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
		versionID := published.Versions[0].ID

		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": []string{"issues:read"}})
			recorder := httptest.NewRecorder()
			testHandler.InstallPlugin(recorder,
				pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
		}()
		go func() {
			defer wait.Done()
			recorder := httptest.NewRecorder()
			testHandler.DeletePluginPackage(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/packages", nil,
				map[string]string{"id": testWorkspaceID, "packageId": published.ID}))
		}()
		wait.Wait()

		var dangling int
		if err := testPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM plugin_installation i
			 WHERE i.workspace_id = $1
			   AND NOT EXISTS (SELECT 1 FROM plugin_package_version v WHERE v.id = i.package_version_id)`,
			testWorkspaceID,
		).Scan(&dangling); err != nil {
			t.Fatalf("count dangling installations: %v", err)
		}
		if dangling != 0 {
			t.Fatalf("attempt %d: %d installation(s) name a version that no longer exists", attempt, dangling)
		}
	}
}

// The mirror case: a publish racing a delete of the same plugin must not leave
// a version whose package row is gone. Such a version is unreachable — the
// settings page lists versions per package — so it would be storage nothing can
// find or clean up.
func TestPublishAndDeleteRaceLeavesNoOrphanVersion(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	withHostCapabilities(t)

	for attempt := range 12 {
		cleanupPluginInstallations(t)
		published := publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")

		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			uploadPluginBundle(t, pluginBundleZip(t, packageManifest("2.0.0"), "console.log('v2');\n"))
		}()
		go func() {
			defer wait.Done()
			recorder := httptest.NewRecorder()
			testHandler.DeletePluginPackage(recorder, pluginHandlerRequest(http.MethodDelete, "/plugins/packages", nil,
				map[string]string{"id": testWorkspaceID, "packageId": published.ID}))
		}()
		wait.Wait()

		var orphans int
		if err := testPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM plugin_package_version v
			 WHERE v.workspace_id = $1
			   AND NOT EXISTS (SELECT 1 FROM plugin_package p WHERE p.id = v.package_id)`,
			testWorkspaceID,
		).Scan(&orphans); err != nil {
			t.Fatalf("count orphan versions: %v", err)
		}
		if orphans != 0 {
			t.Fatalf("attempt %d: %d published version(s) outlived their package", attempt, orphans)
		}
	}
}

// A failed publish must leave the package row exactly as it was. The display
// name follows the newest PUBLISHED version, so a conflicting publish that
// renamed the package would have the list describing an artifact that was never
// stored — and a first publish that failed after creating the row would leave a
// plugin with no versions at all.
func TestFailedPublishDoesNotMovePackageState(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	withHostCapabilities(t)
	cleanupPluginInstallations(t)

	publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")

	// Same version, different display name: the version insert loses the unique
	// index, so nothing about this publish may survive.
	renamed := packageManifest("1.0.0")
	renamed = replaceOnce(t, renamed, `"name": "Published Panel"`, `"name": "Renamed Panel"`)
	recorder := uploadPluginBundle(t, pluginBundleZip(t, renamed, "console.log('tampered');\n"))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("republish status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var name string
	var versions int
	if err := testPool.QueryRow(context.Background(),
		`SELECT p.name, (SELECT COUNT(*) FROM plugin_package_version v WHERE v.package_id = p.id)
		 FROM plugin_package p WHERE p.workspace_id = $1`, testWorkspaceID,
	).Scan(&name, &versions); err != nil {
		t.Fatalf("read package: %v", err)
	}
	if name != "Published Panel" {
		t.Fatalf("a failed publish renamed the package to %q", name)
	}
	if versions != 1 {
		t.Fatalf("versions = %d, want the one that actually published", versions)
	}
}

func replaceOnce(t *testing.T, source, old, replacement string) string {
	t.Helper()
	if !strings.Contains(source, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(source, old, replacement, 1)
}
