package chatstream

import "testing"

func TestArtifactDocumentURLUsesWorkspaceSlug(t *testing.T) {
	got := ArtifactDocumentURL(
		"https://multica.example/",
		"firtal",
		"019f6ecb-60c5-75bb-8a3c-0d8d44f1f224",
	)
	want := "https://multica.example/firtal/documents/019f6ecb-60c5-75bb-8a3c-0d8d44f1f224"
	if got != want {
		t.Fatalf("ArtifactDocumentURL() = %q, want %q", got, want)
	}
}
