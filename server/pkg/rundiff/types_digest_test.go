package rundiff

import "testing"

func TestBodyDigestIsStableAndSensitive(t *testing.T) {
	a := BodyDigest("before", "after")
	if a != BodyDigest("before", "after") {
		t.Fatal("digest must be deterministic")
	}
	if a == BodyDigest("before", "other") || a == BodyDigest("", "after") {
		t.Fatal("digest must distinguish content")
	}
	// A missing side hashes as empty, not as the other side.
	if BodyDigest("", "x") == BodyDigest("x", "") {
		t.Fatal("side order must matter")
	}
}

func TestComputeDigestIsOrderIndependent(t *testing.T) {
	c1 := Empty("9c1b0ac1-0000-4000-8000-00000000d001", "ready", "")
	c1.Files = []File{
		{ID: "1", RootID: "r", Path: "a.txt", Change: "modified", BodySHA256: BodyDigest("one", "two")},
		{ID: "2", RootID: "r", Path: "b.txt", Change: "added", BodySHA256: BodyDigest("", "three")},
	}
	c2 := c1
	c2.Files = []File{c1.Files[1], c1.Files[0]}
	if c1.ComputeDigest() == "" || c1.ComputeDigest() != c2.ComputeDigest() {
		t.Fatal("digest must not depend on file order")
	}
}

func TestVerifyDigest(t *testing.T) {
	c := Empty("9c1b0ac1-0000-4000-8000-00000000d001", "ready", "")
	c.Files = []File{{ID: "1", RootID: "r", Path: "a.txt", Change: "modified", BodySHA256: BodyDigest("x", "y")}}
	c.Digest = c.ComputeDigest()
	c.DigestVersion = DigestVersion
	if !c.VerifyDigest() {
		t.Fatal("fresh digest must verify")
	}
	// Tampering with a file's body hash must break the collection digest.
	c.Files[0].BodySHA256 = BodyDigest("tampered", "y")
	if c.VerifyDigest() {
		t.Fatal("tampered manifest must not verify")
	}
	// No digest at all is "not checked", not "failed".
	c.Digest = ""
	if !c.VerifyDigest() {
		t.Fatal("absent digest means unchecked, which passes")
	}
	// A collection with no verifiable files computes no digest.
	empty := Empty("9c1b0ac1-0000-4000-8000-00000000d001", "ready", "")
	if empty.ComputeDigest() != "" || !empty.VerifyDigest() {
		t.Fatal("unverifiable collection must have no digest and pass")
	}
}

func TestValidateAndSanitizeRejectsBrokenDigest(t *testing.T) {
	id := "9c1b0ac1-0000-4000-8000-00000000d001"
	body := "hello"
	c := Empty(id, "ready", "")
	c.Roots = []Root{{ID: "r", Label: "r", Kind: "directory", Coverage: "complete"}}
	c.Files = []File{{ID: "f", RootID: "r", Path: "a.txt", Change: "modified", BodyStatus: "available", Before: &body, After: &body}}
	c.Files[0].BodySHA256 = BodyDigest("hello", "hello")
	c.Digest = c.ComputeDigest()
	c.DigestVersion = DigestVersion
	if err := c.ValidateAndSanitize(); err != nil {
		t.Fatalf("consistent digest must pass: %v", err)
	}
	// Corrupt the digest after the files were sanitized differently.
	c.Digest = BodyDigest("x", "y")
	c.DigestVersion = DigestVersion
	if err := c.ValidateAndSanitize(); err == nil {
		t.Fatal("digest that does not cover the manifest must be rejected")
	}
	// Bad framing is rejected too.
	c.Digest = "nothex"
	if err := c.ValidateAndSanitize(); err == nil {
		t.Fatal("non-hex digest must be rejected")
	}
}

func TestFileVerifyBody(t *testing.T) {
	f := File{BodyStatus: "available", BodySHA256: BodyDigest("before", "after")}
	if !f.VerifyBody("before", "after") {
		t.Fatal("matching content must verify")
	}
	if f.VerifyBody("before", "tampered") {
		t.Fatal("tampered content must not verify")
	}
	// Absence is "not checked".
	g := File{BodyStatus: "available"}
	if !g.VerifyBody("anything", "at all") {
		t.Fatal("files without a captured digest are unchecked, which passes")
	}
}
