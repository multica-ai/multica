package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContainsImageContentBlock(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty", "", false},
		{"plain text result", `"command finished with exit code 0"`, false},
		{"text content block", `[{"type":"text","text":"hello"}]`, false},
		{"image block in array", `[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]`, true},
		{"bare image block", `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}`, true},
		{"image alongside text", `[{"type":"text","text":"see below"},{"type":"image","source":{"data":"AAAA"}}]`, true},
		// The word appearing in ordinary output must not trip the detector:
		// misreading a log line as an image would replace it with a
		// placeholder and destroy the very content the log exists to show.
		{"text merely mentioning images", `"failed to load \"image\" from disk"`, false},
		{"json with image-like key", `{"type":"text","text":"{\"image\":\"foo\"}"}`, false},
		{"malformed json", `[{"type":"image"`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsImageContentBlock(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("containsImageContentBlock(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestContainsImageContentBlockLargePayload(t *testing.T) {
	block := `[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` +
		strings.Repeat("A", 60000) + `"}}]`
	if !containsImageContentBlock(json.RawMessage(block)) {
		t.Error("a real screenshot payload was not recognised as an image")
	}
}

func TestToolResultOutput(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOut   string
		wantImage bool
	}{
		{"empty", "", "", false},
		// Transport encoding, not content: the reader should see the terminal
		// text, and the byte count should measure that rather than its escaped
		// form. Left wrapped, "\n" is two characters and the preview fills with
		// literal backslash-n once cut.
		{"json string is unwrapped", `"line one\nline two"`, "line one\nline two", false},
		{"escaped quotes survive", `"he said \"hi\""`, `he said "hi"`, false},
		{"bare document passes through", `{"ok":true}`, `{"ok":true}`, false},
		{"plain text passes through", `not json at all`, `not json at all`, false},
		{"text content block", `[{"type":"text","text":"hello"}]`, `[{"type":"text","text":"hello"}]`, false},
		{
			"image content block is marked",
			`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]`,
			`[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]`,
			true,
		},
		// A decoded string is text by construction, however image-ish it reads.
		{"quoted text mentioning image", `"failed to load image data"`, "failed to load image data", false},
		// json.Unmarshal puts `null` into a string variable without error,
		// leaving the zero value. Inferring "this was a JSON string" from the
		// decode succeeding therefore turns a legal tool result into empty
		// output, which omitempty drops and the UI renders as nothing — the
		// whole result vanishes from the transcript.
		{"null is not a string", `null`, `null`, false},
		{"padded null is not a string", " null", " null", false},
		// The other literals already failed to decode, but they belong in the
		// table so the type check is exercised rather than assumed.
		{"true passes through", `true`, `true`, false},
		{"number passes through", `123`, `123`, false},
		{"empty array passes through", `[]`, `[]`, false},
		{"empty object passes through", `{}`, `{}`, false},
		{"empty json string decodes to empty", `""`, ``, false},
		{"whitespace-padded json string", "  \"padded\"  ", "padded", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isImage := ToolResultOutput(json.RawMessage(tc.raw))
			if out != tc.wantOut {
				t.Errorf("output = %q, want %q", out, tc.wantOut)
			}
			if isImage != tc.wantImage {
				t.Errorf("isImage = %v, want %v", isImage, tc.wantImage)
			}
		})
	}
}

// The byte count reported with a truncated preview is meant to describe the
// output the reader saw. Measuring the wire form instead makes the same content
// report different sizes depending on how much escaping its provider applied.
func TestToolResultOutputSizeIsLogical(t *testing.T) {
	logical := strings.Repeat("line with \"quotes\" and \n newlines\n", 100)
	encoded, err := json.Marshal(logical)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= len(logical) {
		t.Fatal("fixture does not exercise escaping growth")
	}

	out, _ := ToolResultOutput(json.RawMessage(encoded))
	if len(out) != len(logical) {
		t.Errorf("logical size = %d, want %d (wire form is %d)", len(out), len(logical), len(encoded))
	}
}
