package pluginbundled

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestBundledCatalogLoadsSignedOfficialReleases(t *testing.T) {
	catalog := Load()
	if diagnostics := catalog.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	entries := catalog.List()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	latest, ok := catalog.Latest("ai.multica.software-delivery")
	if !ok || latest.Release.Manifest.Metadata.Version != "1.1.0" {
		t.Fatalf("latest = %+v, %v", latest.Release.Manifest.Metadata, ok)
	}
	if !latest.Compatible || latest.PublisherType != PublisherTypeOfficial || latest.TrustTier != TrustTierOfficial {
		t.Fatalf("unexpected admission = %+v", latest)
	}
	if latest.Release.ArtifactDigest != "sha256:c0064980c253c567f9df90999b158c670989bb60d213289d2f3c36b9a8b591ff" {
		t.Fatalf("artifact digest = %s", latest.Release.ArtifactDigest)
	}
	if len(latest.Release.Contributions) != 1 || latest.Release.Contributions[0].Key != "review-readiness" {
		t.Fatalf("contributions = %+v", latest.Release.Contributions)
	}
}

func TestBundledCatalogIsolatesTamperedRelease(t *testing.T) {
	copyFS := make(fstest.MapFS)
	if err := fs.WalkDir(bundledFS, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, readErr := fs.ReadFile(bundledFS, name)
		if readErr != nil {
			return readErr
		}
		copyFS[name] = &fstest.MapFile{Data: append([]byte(nil), content...), Mode: 0o600}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tampered := "releases/ai.multica.software-delivery/1.1.0/skills/review-readiness/SKILL.md"
	copyFS[tampered].Data = append(copyFS[tampered].Data, []byte("\ntampered")...)

	catalog := LoadFromFS(copyFS)
	if len(catalog.List()) != 1 {
		t.Fatalf("healthy releases = %d, want 1", len(catalog.List()))
	}
	if _, ok := catalog.Find("ai.multica.software-delivery", "1.1.0"); ok {
		t.Fatal("tampered release was admitted")
	}
	if diagnostics := catalog.Diagnostics(); len(diagnostics) != 1 || diagnostics[0].Code != "signature_invalid" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{candidate: "1.1.0", current: "1.0.0", want: true},
		{candidate: "2.0.0", current: "1.9.9", want: true},
		{candidate: "1.0.0", current: "1.0.0", want: false},
		{candidate: "1.0.0", current: "1.1.0", want: false},
		{candidate: "1.0.0", current: "1.0.0-rc.1", want: true},
		{candidate: "1.0.0-rc.10", current: "1.0.0-rc.9", want: true},
		{candidate: "1.0.0-beta.1", current: "1.0.0-1", want: true},
		{candidate: "1.0.0-rc.1", current: "1.0.0-rc.1.1", want: false},
		{candidate: "1.2.1+r1", current: "1.2.0", want: true},
		{candidate: "1.2.1+r2", current: "1.2.1+r1", want: false},
		{candidate: "100000000000000000000.0.0", current: "99999999999999999999.0.0", want: true},
	}
	for _, test := range tests {
		if got := IsNewerVersion(test.candidate, test.current); got != test.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}
