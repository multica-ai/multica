package plugintest

import (
	"strings"
	"testing"
)

func TestReviewReadinessReleaseIsSignedAndValidated(t *testing.T) {
	release, err := ReviewReadinessRelease()
	if err != nil {
		t.Fatalf("ReviewReadinessRelease: %v", err)
	}
	if release.Manifest.Metadata.Key != ReviewReadinessPluginKey || release.Manifest.Metadata.Version != ReviewReadinessVersion {
		t.Fatalf("unexpected release identity: %s@%s", release.Manifest.Metadata.Key, release.Manifest.Metadata.Version)
	}
	if len(release.Signature) == 0 || len(release.Files) != 2 {
		t.Fatalf("signature/files = %d/%d", len(release.Signature), len(release.Files))
	}
	if !strings.Contains(string(release.Files[1].Content), "# Review readiness") {
		t.Fatal("reference review-readiness skill content missing")
	}
}
