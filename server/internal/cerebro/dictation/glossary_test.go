package dictation

import "testing"

func TestMergeGlossaryKeepsManualTermsFirstAndDeduplicates(t *testing.T) {
	got := mergeGlossary(
		"Helsebixen, made4men",
		"Mia, made4men, Multica Features",
		"Helsebixen, Supplier Name",
	)
	want := "Helsebixen, made4men, Mia, Multica Features, Supplier Name"
	if got != want {
		t.Fatalf("mergeGlossary = %q, want %q", got, want)
	}
}

func TestBusinessObjectGlossaryIsDisabledWithoutProject(t *testing.T) {
	t.Setenv(envBusinessObjectsBQProject, "")
	if got := businessObjectGlossary(t.Context()); got != "" {
		t.Fatalf("businessObjectGlossary = %q, want empty", got)
	}
}
