package cloud

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// drainLineBuffer simulates the PTY line buffer logic by feeding chunks
// through drainPTYData and collecting parsed messages.
func drainLineBuffer(chunks []string) ([]agent.Message, string, string) {
	msgCh := make(chan agent.Message, 256)
	dataCh := make(chan []byte, len(chunks))
	for _, c := range chunks {
		dataCh <- []byte(c)
	}
	close(dataCh)

	var textOutput, toolOutput strings.Builder
	drainPTYData(context.Background(), dataCh, msgCh, &textOutput, &toolOutput)
	close(msgCh)

	var msgs []agent.Message
	for m := range msgCh {
		msgs = append(msgs, m)
	}
	return msgs, textOutput.String(), toolOutput.String()
}

func TestLineBuffer_SingleComplete(t *testing.T) {
	msgs, output, _ := drainLineBuffer([]string{
		"{\"type\": \"assistant_delta\", \"text\": \"Hello\"}\n",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != agent.MessageText || msgs[0].Content != "Hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
	if output != "Hello" {
		t.Errorf("expected output 'Hello', got %q", output)
	}
}

func TestLineBuffer_SplitAcrossChunks(t *testing.T) {
	msgs, _, _ := drainLineBuffer([]string{
		"{\"type\": \"assistant_del",
		"ta\", \"text\": \"Split\"}\n",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Split" {
		t.Errorf("expected content 'Split', got %q", msgs[0].Content)
	}
}

func TestLineBuffer_MultipleInOneChunk(t *testing.T) {
	msgs, output, _ := drainLineBuffer([]string{
		"{\"type\": \"assistant_delta\", \"text\": \"A\"}\n{\"type\": \"assistant_delta\", \"text\": \"B\"}\n",
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "A" || msgs[1].Content != "B" {
		t.Errorf("unexpected messages: %+v, %+v", msgs[0], msgs[1])
	}
	if output != "AB" {
		t.Errorf("expected output 'AB', got %q", output)
	}
}

func TestLineBuffer_ANSIWrapped(t *testing.T) {
	msgs, _, _ := drainLineBuffer([]string{
		"\x1b[?2004l\r{\"type\": \"assistant_delta\", \"text\": \"4\"}\r\n",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "4" {
		t.Errorf("expected content '4', got %q", msgs[0].Content)
	}
}

func TestLineBuffer_NonJSONSkipped(t *testing.T) {
	msgs, _, _ := drainLineBuffer([]string{
		"root@sandbox:~# \n",
		"oh -p \"hello\" --output-format stream-json\n",
		"{\"type\": \"assistant_delta\", \"text\": \"Hi\"}\n",
		"exit\n",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (non-JSON skipped), got %d", len(msgs))
	}
	if msgs[0].Content != "Hi" {
		t.Errorf("expected content 'Hi', got %q", msgs[0].Content)
	}
}

func TestLineBuffer_EmptyChunks(t *testing.T) {
	msgs, _, _ := drainLineBuffer([]string{
		"",
		"",
		"{\"type\": \"assistant_delta\", \"text\": \"Ok\"}\n",
		"",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestLineBuffer_FlushRemaining(t *testing.T) {
	msgs, _, _ := drainLineBuffer([]string{
		"{\"type\": \"assistant_delta\", \"text\": \"Partial\"}",
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from flush, got %d", len(msgs))
	}
	if msgs[0].Content != "Partial" {
		t.Errorf("expected content 'Partial', got %q", msgs[0].Content)
	}
}

func TestLineBuffer_ContextCancellation(t *testing.T) {
	msgCh := make(chan agent.Message, 256)
	dataCh := make(chan []byte, 10)

	// Buffer a message BEFORE creating context so it's available on first select
	dataCh <- []byte("{\"type\": \"assistant_delta\", \"text\": \"Before\"}\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — drainPTYData should still not hang

	var textOutput, toolOutput strings.Builder
	done := make(chan struct{})
	go func() {
		drainPTYData(ctx, dataCh, msgCh, &textOutput, &toolOutput)
		close(done)
	}()

	// Must complete within 2 seconds (proves no infinite hang)
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("drainPTYData hung after context cancellation")
	}
	close(msgCh)

	var msgs []agent.Message
	for m := range msgCh {
		msgs = append(msgs, m)
	}
	t.Logf("got %d messages after context cancel", len(msgs))
	// With cancelled context, drainPTYData may or may not process the buffered
	// message (depends on select ordering). The key assertion is it doesn't hang.
}

func TestLineBuffer_OutputAccumulation(t *testing.T) {
	msgs, output, _ := drainLineBuffer([]string{
		"{\"type\": \"assistant_delta\", \"text\": \"Part 1\"}\n",
		"{\"type\": \"tool_started\", \"tool_name\": \"web_search\", \"tool_input\": {\"query\": \"test\"}}\n",
		"{\"type\": \"assistant_delta\", \"text\": \" Part 2\"}\n",
	})
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Output should only contain text messages, not tool events
	if output != "Part 1 Part 2" {
		t.Errorf("expected output 'Part 1 Part 2', got %q", output)
	}
}

func TestLineBuffer_ToolOutputFallback(t *testing.T) {
	// When no text messages exist, tool output should be collected as fallback
	msgs, textOutput, toolOutput := drainLineBuffer([]string{
		"{\"type\": \"tool_started\", \"tool_name\": \"web_search\", \"tool_input\": {\"query\": \"test\"}}\n",
		"{\"type\": \"tool_completed\", \"tool_name\": \"web_search\", \"output\": \"Result: found 5 items\", \"is_error\": false}\n",
		"{\"type\": \"tool_started\", \"tool_name\": \"web_fetch\", \"tool_input\": {\"url\": \"https://example.com\"}}\n",
		"{\"type\": \"tool_completed\", \"tool_name\": \"web_fetch\", \"output\": \"Page content here\", \"is_error\": false}\n",
	})
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	// No text output (no assistant_delta/complete events)
	if textOutput != "" {
		t.Errorf("expected empty text output, got %q", textOutput)
	}
	// Tool output should contain both results
	if !strings.Contains(toolOutput, "Result: found 5 items") {
		t.Errorf("tool output missing first result: %q", toolOutput)
	}
	if !strings.Contains(toolOutput, "Page content here") {
		t.Errorf("tool output missing second result: %q", toolOutput)
	}
}

// ---------------------------------------------------------------------------
// detectContentType tests
// ---------------------------------------------------------------------------

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		data     []byte
		want     string
	}{
		// Extension override map
		{"markdown", "report.md", []byte("# Hello"), "text/markdown"},
		{"csv", "data.csv", []byte("a,b,c"), "text/csv"},
		{"json", "config.json", []byte(`{"key":"value"}`), "application/json"},
		{"svg", "icon.svg", []byte("<svg></svg>"), "image/svg+xml"},

		// Standard mime package
		{"pdf", "doc.pdf", []byte("%PDF-1.4"), "application/pdf"},
		{"txt", "notes.txt", []byte("plain text"), "text/plain; charset=utf-8"},
		{"html", "page.html", []byte("<html></html>"), "text/html; charset=utf-8"},

		// Case insensitivity
		{"uppercase ext", "REPORT.MD", []byte("# Title"), "text/markdown"},
		{"mixed case", "Data.CSV", []byte("x,y"), "text/csv"},

		// Fallback to content sniffing (no extension)
		{"no extension html", "file", []byte("<html><body>hi</body></html>"), "text/html; charset=utf-8"},
		{"no extension binary", "file", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},

		// Known extension via mime package (not in our override map)
		{"xyz chemical", "data.xyz", []byte("just plain text here"), "chemical/x-xyz"},
		// Truly unknown extension — falls through to content sniffing
		{"unknown ext text", "data.zzz123", []byte("just plain text here"), "text/plain; charset=utf-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectContentType(tt.filename, tt.data)
			if got != tt.want {
				t.Errorf("detectContentType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestExtractArtifacts_Constants(t *testing.T) {
	// Verify constants are sensible
	if maxArtifactSize != 5<<20 {
		t.Errorf("maxArtifactSize = %d, want %d (5MB)", maxArtifactSize, 5<<20)
	}
	if maxTotalArtifacts != 20<<20 {
		t.Errorf("maxTotalArtifacts = %d, want %d (20MB)", maxTotalArtifacts, 20<<20)
	}
	if artifactOutputDir != "/workspace/output/" {
		t.Errorf("artifactOutputDir = %q, want %q", artifactOutputDir, "/workspace/output/")
	}
}

func TestBuildEntrypointScript_ContainsGemini(t *testing.T) {
	script := buildEntrypointScript("test prompt", "auto-fastest", 10, "", "test-api-key")
	if !strings.Contains(script, "gemini-2.5-flash") {
		t.Error("entrypoint script should contain gemini-2.5-flash model")
	}
	if !strings.Contains(script, "generativelanguage.googleapis.com") {
		t.Error("entrypoint script should contain Google AI Studio base URL")
	}
	if !strings.Contains(script, "test-api-key") {
		t.Error("entrypoint script should contain the injected API key")
	}
	if !strings.Contains(script, "--output-format stream-json") {
		t.Error("entrypoint script should pass --output-format stream-json to oh")
	}
	if !strings.Contains(script, "--api-format openai") {
		t.Error("entrypoint script should pass --api-format openai to oh")
	}
	// Verify write_file compliance instructions are in the prompt
	if !strings.Contains(script, "MUST be write_file") {
		t.Error("entrypoint script should contain write_file compliance instruction")
	}
	// Verify search proxy is started
	if !strings.Contains(script, "search-proxy.py") {
		t.Error("entrypoint script should start the search proxy")
	}
	if !strings.Contains(script, "localhost:8888") {
		t.Error("entrypoint script should reference search proxy URL")
	}
}

func TestBuildEntrypointScript_NoAPIKey(t *testing.T) {
	script := buildEntrypointScript("test prompt", "auto-fastest", 10, "", "")
	// With no API key, OPENAI_API_KEY should be empty (OH will fail at startup)
	if !strings.Contains(script, `OPENAI_API_KEY=''`) {
		t.Error("entrypoint script without API key should have empty OPENAI_API_KEY")
	}
	// Should still reference Gemini base URL (just with empty key)
	if !strings.Contains(script, "generativelanguage.googleapis.com") {
		t.Error("entrypoint script should always contain Google AI Studio base URL")
	}
}

// ---------------------------------------------------------------------------
// sanitizeFilename tests
// ---------------------------------------------------------------------------

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"simple", "report.md", "report.md", true},
		{"with spaces", "my report.md", "my report.md", true},
		{"path traversal", "../../etc/passwd", "", false},
		{"path traversal subtle", "foo/../../../etc/shadow", "", false},
		{"absolute path", "/etc/passwd", "", false},
		{"null byte", "file\x00.md", "", false},
		{"control chars", "file\x01\x02.md", "", false},
		{"directory slash", "subdir/file.md", "", false},
		{"backslash", "sub\\file.md", "", false},
		{"dot dot", "..", "", false},
		{"single dot", ".", "", false},
		{"empty", "", "", false},
		{"leading dot", ".hidden", ".hidden", true},
		{"unicode", "报告.md", "报告.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sanitizeFilename(tt.input)
			if ok != tt.ok {
				t.Errorf("sanitizeFilename(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isAllowedFileType tests
// ---------------------------------------------------------------------------

func TestIsAllowedFileType(t *testing.T) {
	tests := []struct {
		filename string
		allowed  bool
	}{
		{"report.md", true},
		{"data.csv", true},
		{"config.json", true},
		{"notes.txt", true},
		{"doc.pdf", true},
		{"sheet.xlsx", true},
		{"REPORT.MD", true},   // case insensitive
		{"data.CSV", true},
		{"script.sh", false},
		{"binary.exe", false},
		{"app.bin", false},
		{"image.png", false},
		{"archive.zip", false},
		{"noextension", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := isAllowedFileType(tt.filename); got != tt.allowed {
				t.Errorf("isAllowedFileType(%q) = %v, want %v", tt.filename, got, tt.allowed)
			}
		})
	}
}

func TestBuildEntrypointScript_SystemPrompt(t *testing.T) {
	script := buildEntrypointScript("test prompt", "auto-fastest", 10, "Be helpful", "key")
	if !strings.Contains(script, "ADDITIONAL INSTRUCTIONS: Be helpful") {
		t.Error("entrypoint script should include system prompt")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"it's here", "'it'\\''s here'"},
		{"", "''"},
		{"hello world", "'hello world'"},
		{"$(whoami)", "'$(whoami)'"},
		{"`id`", "'`id`'"},
		{"a;b", "'a;b'"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
