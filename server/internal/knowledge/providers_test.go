package knowledge

import "testing"

func TestValidateBaseModelPurpose(t *testing.T) {
	if err := validateBaseModelPurpose(PurposeMain); err == nil {
		t.Fatal("base-level main binding was accepted")
	}
	for _, purpose := range []string{PurposeExtract, PurposeAnswer, PurposeParse, PurposeEmbedding, PurposeRerank} {
		if err := validateBaseModelPurpose(purpose); err != nil {
			t.Fatalf("base purpose %q was rejected: %v", purpose, err)
		}
	}
}
