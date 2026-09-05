package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/redact"
)

// noticeLen is the size of the in-band marker for a given original size. The
// marker's bytes come out of the budget, so a truncated preview can never keep
// a full budget's worth of original text.
func noticeLen(originalBytes int) int {
	return len(fmt.Sprintf(toolOutputTruncatedNotice, originalBytes))
}

func TestToolOutputPreviewShortOutputUnchanged(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"plain text", "hello world"},
		{"multiline", "line one\nline two\n"},
		{"chinese", "执行完成，共处理 42 条记录"},
		{"one byte under budget", strings.Repeat("a", toolOutputPreviewBudget-1)},
		{"exactly budget", strings.Repeat("a", toolOutputPreviewBudget)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated, originalBytes := toolOutputPreview(tc.in, false)
			if got != tc.in {
				t.Errorf("output changed; short output must be byte-for-byte identical")
			}
			if truncated {
				t.Errorf("truncated = true for a %d byte output", len(tc.in))
			}
			if originalBytes != len(tc.in) {
				t.Errorf("originalBytes = %d, want %d", originalBytes, len(tc.in))
			}
		})
	}
}

func TestToolOutputPreviewTruncatesAboveBudget(t *testing.T) {
	in := strings.Repeat("a", toolOutputPreviewBudget+1)
	got, truncated, originalBytes := toolOutputPreview(in, false)

	if !truncated {
		t.Error("truncated = false one byte over budget")
	}
	if originalBytes != toolOutputPreviewBudget+1 {
		t.Errorf("originalBytes = %d, want %d", originalBytes, toolOutputPreviewBudget+1)
	}
	if len(got) > toolOutputPreviewBudget {
		t.Errorf("preview is %d bytes, over the %d budget", len(got), toolOutputPreviewBudget)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("preview carries no in-band notice; a server that drops the " +
			"structured fields would render this as a complete output")
	}
	if !strings.Contains(got, fmt.Sprint(originalBytes)) {
		t.Error("in-band notice omits the original size")
	}
}

// The persisted preview is what the server writes after its own redaction pass,
// so that is the string the budget has to hold. Redaction can grow text — a
// 7-byte "TOKEN=x" becomes a 21-byte placeholder — so an input that fits the
// budget before redaction can exceed it afterwards. Judging the raw length
// alone would store an over-budget row while reporting truncated=false.
func TestToolOutputPreviewAccountsForRedactionGrowth(t *testing.T) {
	unit := "TOKEN=x "
	in := strings.Repeat(unit, toolOutputPreviewBudget/len(unit))
	if len(in) > toolOutputPreviewBudget {
		t.Fatalf("test input is %d bytes; it must start within budget", len(in))
	}
	if len(redact.Text(in)) <= toolOutputPreviewBudget {
		t.Fatal("test input does not expand past the budget under redaction")
	}

	got, truncated, _ := toolOutputPreview(in, false)
	if !truncated {
		t.Error("truncated = false, but the persisted preview exceeds the budget")
	}
	if persisted := redact.Text(got); len(persisted) > toolOutputPreviewBudget {
		t.Errorf("persisted preview is %d bytes after server redaction, over the %d budget",
			len(persisted), toolOutputPreviewBudget)
	}
}

// A byte budget applied to multi-byte text lands inside a rune roughly
// (encoding width - 1) times out of every width. 8192 % 3 == 2, so Chinese
// crosses a rune every time; 8192 % 4 == 0, so pure emoji aligns exactly and on
// its own would not exercise this at all. The mixed case is the one that
// actually tests the boundary.
func TestToolOutputPreviewKeepsValidUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"chinese", strings.Repeat("中", 5000)},
		{"emoji", strings.Repeat("😀", 5000)},
		{"chinese then emoji", strings.Repeat("中", 2700) + strings.Repeat("😀", 400)},
		{"emoji then chinese", strings.Repeat("😀", 2100) + strings.Repeat("中", 400)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated, _ := toolOutputPreview(tc.in, false)
			if !truncated {
				t.Fatalf("input of %d bytes was not truncated", len(tc.in))
			}
			if !utf8.ValidString(got) {
				t.Error("preview is not valid UTF-8")
			}
			if strings.ContainsRune(got, utf8.RuneError) {
				t.Error("preview contains U+FFFD; the cut split a rune instead of " +
					"backing off to a boundary")
			}
			if len(got) > toolOutputPreviewBudget {
				t.Errorf("preview is %d bytes, over budget", len(got))
			}
		})
	}
}

// Input that is already invalid cannot also be preserved byte-for-byte: the
// two guarantees are in direct conflict, and storable text wins. What must not
// happen is a panic — the normalisation path once ran before the length check,
// so a one-byte invalid input took the truncation branch and sliced ~8 KiB out
// of a 1-byte string.
func TestToolOutputPreviewHandlesInvalidUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"single invalid byte", "\xff"},
		{"short invalid run", "ab\xff\xfe\xfdcd"},
		{"invalid bytes spread out", "a\xffb\xffc\xffd"},
		{"nul byte", "before\x00after"},
		{"invalid just under budget", strings.Repeat("a", toolOutputPreviewBudget-2) + "\xff"},
		{"invalid just over budget", strings.Repeat("a", toolOutputPreviewBudget) + "\xff\xfe"},
		{"truncated rune at end", "ok text " + string([]byte{0xE4, 0xB8})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, originalBytes := toolOutputPreview(tc.in, false)
			if !utf8.ValidString(got) {
				t.Error("preview is not valid UTF-8")
			}
			if strings.ContainsRune(got, 0) {
				t.Error("preview still contains a NUL byte; PostgreSQL rejects those")
			}
			if originalBytes != len(tc.in) {
				t.Errorf("originalBytes = %d, want %d (measured before normalisation)",
					originalBytes, len(tc.in))
			}
			if len(got) > toolOutputPreviewBudget {
				t.Errorf("preview is %d bytes, over budget", len(got))
			}
		})
	}
}

// A cut that is safe but discards everything is useless: the Execution log
// exists to be read. These shapes have no whitespace near the boundary, which
// is where an over-eager rule erases the whole preview.
func TestToolOutputPreviewKeepsUsablePrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"single line ascii", strings.Repeat("abcdefghij", 5000)},
		{"compact json", `{"key":"` + strings.Repeat("v", 40000) + `"}`},
		{"base64 blob", strings.Repeat("QUJDREVGR0hJSktMTU5PUFFS", 2000)},
		{"chinese without spaces", strings.Repeat("中", 5000)},
		{"chinese prose", strings.Repeat("这是一段中文日志 输出内容 ", 700)},
		{"go stack trace", strings.Repeat("goroutine 1 [running]:\nmain.foo(0x1)\n\t/src/a.go:42 +0x1f\n", 300)},
		{"unified diff", strings.Repeat("+ added line of code here\n- removed line here\n", 300)},
	}
	const floor = 8000
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated, _ := toolOutputPreview(tc.in, false)
			if !truncated {
				t.Fatalf("input of %d bytes was not truncated", len(tc.in))
			}
			if len(got) < floor {
				t.Errorf("kept only %d bytes; a preview this short tells the reader "+
					"nothing about what the tool produced", len(got))
			}
			if len(got) > toolOutputPreviewBudget {
				t.Errorf("preview is %d bytes, over budget", len(got))
			}
		})
	}
}

// A straddling credential must not survive in the preview. The yardstick is
// redacting the whole input: this change may not expose more than that would.
//
// The assertion is on a recognisable PREFIX, not the whole secret — truncation
// removes the whole secret by definition, so asserting on that passes for any
// implementation while a leaked head goes unnoticed.
func TestToolOutputPreviewDoesNotLeakStraddlingSecrets(t *testing.T) {
	tail := strings.Repeat(" filler", 4000)
	tests := []struct {
		name    string
		secret  string
		witness string
	}{
		{"postgres password", "postgres://user:" + strings.Repeat("S", 20000) + "@host/db", "postgres://user:SSSS"},
		{"jwt long first segment", "ey" + strings.Repeat("A", 20000) + ".sig.sig", "ey" + strings.Repeat("A", 200)},
		{"bearer token", "Bearer " + strings.Repeat("t", 20000), "Bearer tttttttttttt"},
		{"github token", "ghp_" + strings.Repeat("k", 20000), "ghp_kkkkkkkkkkkk"},
		{"pem private key", "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIE\n", 20000), "BEGIN RSA PRIVATE KEY"},
		{"generic credential", "PASSWORD=" + strings.Repeat("z", 20000), "PASSWORD=zzzz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Place the secret so it starts inside the window and runs past it.
			lead := strings.Repeat("x", toolOutputPreviewBudget-3000) + " "
			in := lead + tc.secret + tail

			preview, _, _ := toolOutputPreview(in, false)
			// The server redacts again on ingest; assert on what finally lands.
			persisted := redact.Text(preview)
			baseline := redact.Text(in)

			if strings.Contains(persisted, tc.witness) && !strings.Contains(baseline, tc.witness) {
				t.Errorf("preview leaks %q, which redacting the full output removes",
					tc.witness[:min(40, len(tc.witness))])
			}
		})
	}
}

// Small images render fine today. Replacing every image with a placeholder
// would take that away, so only oversized ones are swapped — anything else
// would be a regression dressed up as a fix.
func TestToolOutputPreviewImageWithinBudgetIsUntouched(t *testing.T) {
	block := imageBlockJSON(300)
	if len(block) > toolOutputPreviewBudget {
		t.Fatalf("fixture is %d bytes; it must fit the budget", len(block))
	}

	got, truncated, originalBytes := toolOutputPreview(block, true)
	if got != block {
		t.Error("image within budget was altered; it must stay renderable")
	}
	if truncated {
		t.Error("truncated = true for an image that fits")
	}
	if originalBytes != len(block) {
		t.Errorf("originalBytes = %d, want %d", originalBytes, len(block))
	}
}

func TestToolOutputPreviewOversizeImageBecomesPlaceholder(t *testing.T) {
	block := imageBlockJSON(40000)
	if len(block) <= toolOutputPreviewBudget {
		t.Fatalf("fixture is %d bytes; it must exceed the budget", len(block))
	}

	got, truncated, originalBytes := toolOutputPreview(block, true)
	if !truncated {
		t.Error("truncated = false for an oversized image")
	}
	if strings.Contains(got, "iVBOR") || strings.Contains(got, "AAAA") {
		t.Error("placeholder still contains base64; a sliced image is neither a " +
			"picture nor readable text")
	}
	if !strings.Contains(got, fmt.Sprint(originalBytes)) {
		t.Error("placeholder omits the original size")
	}
	if len(got) > toolOutputPreviewBudget {
		t.Errorf("placeholder is %d bytes, over budget", len(got))
	}
}

func imageBlockJSON(dataBytes int) string {
	block := []map[string]any{{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/png",
			"data":       strings.Repeat("A", dataBytes),
		},
	}}
	encoded, err := json.Marshal(block)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
