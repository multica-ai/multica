package agentoffice

import (
	"strings"
	"testing"
)

func TestParseManagedRegionAbsent(t *testing.T) {
	region, err := ParseManagedRegion("# CLAUDE.md\n\nJust code facts.\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != nil {
		t.Fatalf("expected no region, got %+v", region)
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	body := "Agents: propose behavior rules via Agent Office.\nSee harness binding X.\n"
	block := RenderManagedRegion(3, body)
	region, err := ParseManagedRegion(block)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region == nil {
		t.Fatal("expected a region")
	}
	if region.Version != 3 {
		t.Errorf("version = %d, want 3", region.Version)
	}
	if region.Body != body {
		t.Errorf("body = %q, want %q", region.Body, body)
	}
	if !region.Verify() {
		t.Error("freshly rendered region must verify")
	}
}

func TestRenderCanonicalizesCRLFAndMissingTerminator(t *testing.T) {
	// CRLF + missing trailing newline must produce the same seal as the
	// canonical LF-terminated body.
	canonical := RenderManagedRegion(1, "line one\nline two\n")
	messy := RenderManagedRegion(1, "line one\r\nline two")
	if canonical != messy {
		t.Errorf("canonicalization mismatch:\n%q\nvs\n%q", canonical, messy)
	}
}

func TestVerifyDetectsHandEdit(t *testing.T) {
	block := RenderManagedRegion(2, "Do not bypass gates.\n")
	tampered := strings.Replace(block, "Do not", "Please do not", 1)
	region, err := ParseManagedRegion(tampered)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region.Verify() {
		t.Error("hand-edited body must fail verification")
	}
}

func TestParseEmptyBody(t *testing.T) {
	block := RenderManagedRegion(1, "")
	region, err := ParseManagedRegion(block)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region.Body != "" {
		t.Errorf("body = %q, want empty", region.Body)
	}
	if !region.Verify() {
		t.Error("empty body must verify")
	}
}

func TestParseMalformed(t *testing.T) {
	cases := map[string]string{
		"start without end": "<!-- agent-office:start v1 sha256:" + strings.Repeat("a", 64) + " -->\nbody\n",
		"end without start": "text\n<!-- agent-office:end -->\n",
		"bad start marker":  "<!-- agent-office:start v1 sha256:short -->\n<!-- agent-office:end -->\n",
		"two regions":       RenderManagedRegion(1, "a\n") + "\n" + RenderManagedRegion(2, "b\n"),
		"nested start":      "<!-- agent-office:start v1 sha256:" + strings.Repeat("a", 64) + " -->\n<!-- agent-office:start v2 sha256:" + strings.Repeat("b", 64) + " -->\n<!-- agent-office:end -->\n",
	}
	for name, content := range cases {
		if _, err := ParseManagedRegion(content); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUpsertAppendsWhenAbsent(t *testing.T) {
	content := "# CLAUDE.md\n\nBuild: make check\n"
	out, err := UpsertManagedRegion(content, 1, "Agent guidance.\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, content) {
		t.Error("human-owned content must be preserved byte-for-byte")
	}
	region, err := ParseManagedRegion(out)
	if err != nil || region == nil {
		t.Fatalf("expected a region in output, err=%v", err)
	}
	if region.Version != 1 || !region.Verify() {
		t.Errorf("bad appended region: %+v", region)
	}
}

func TestUpsertAppendsWithSeparatorWhenNoTrailingNewline(t *testing.T) {
	out, err := UpsertManagedRegion("# CLAUDE.md", 1, "x\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "# CLAUDE.md\n\n<!-- agent-office:start") {
		t.Errorf("expected blank-line separation, got %q", out)
	}
}

func TestUpsertReplacesExistingRegion(t *testing.T) {
	content := "# Header\n\n" + RenderManagedRegion(1, "old rule\n") + "\n# Footer\n"
	out, err := UpsertManagedRegion(content, 2, "new rule\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(out, "# Header\n\n") || !strings.HasSuffix(out, "\n# Footer\n") {
		t.Errorf("surrounding content not preserved: %q", out)
	}
	region, err := ParseManagedRegion(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region.Version != 2 || region.Body != "new rule\n" || !region.Verify() {
		t.Errorf("bad replaced region: %+v", region)
	}
	if strings.Contains(out, "old rule") {
		t.Error("old body must be gone")
	}
}

func TestUpsertOnEmptyFile(t *testing.T) {
	out, err := UpsertManagedRegion("", 1, "rule\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	region, err := ParseManagedRegion(out)
	if err != nil || region == nil || !region.Verify() {
		t.Fatalf("expected valid region, err=%v region=%+v", err, region)
	}
}
