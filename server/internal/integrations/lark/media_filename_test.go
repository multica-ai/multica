package lark

// media_filename_test.go — the stored object's display name. Not a path
// traversal (path.Base already resolves the path), but a name that reaches
// Content-Disposition and anywhere the file is later re-saved.

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestCleanFilenameRejectsDotOnlyNames(t *testing.T) {
	for _, in := range []string{"..", "...", ".", "  ..  ", "../", "a/../.."} {
		if got := cleanFilename(in); strings.Trim(got, ".") == "" && got != "" {
			t.Errorf("cleanFilename(%q) = %q — a name made only of dots is not a filename", in, got)
		}
	}
	// The ordinary cases must survive, or every upload loses its real name.
	for in, want := range map[string]string{
		"report.pdf":            "report.pdf",
		"/tmp/report.pdf":       "report.pdf",
		`C:\Users\a\report.pdf`: "report.pdf",
		"../../etc/passwd":      "passwd",
		"..hidden.txt":          "..hidden.txt",
	} {
		if got := cleanFilename(in); got != want {
			t.Errorf("cleanFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMediaExtensionKnowsPDF(t *testing.T) {
	if got := mediaExtension("application/pdf"); got != ".pdf" {
		t.Errorf("mediaExtension(application/pdf) = %q, want .pdf — mime.ExtensionsByType reads /etc/mime.types, which a slim image does not ship", got)
	}
	if got := mediaExtension("application/pdf; charset=binary"); got != ".pdf" {
		t.Errorf("parameterized content type = %q, want .pdf", got)
	}
}

func TestMediaExtensionKnowsCommonAudioTypes(t *testing.T) {
	for contentType, want := range map[string]string{
		"audio/opus": ".opus",
		"audio/ogg":  ".ogg",
		"audio/amr":  ".amr",
		"audio/mpeg": ".mp3",
	} {
		if got := mediaExtension(contentType); got != want {
			t.Errorf("mediaExtension(%q) = %q, want %q", contentType, got, want)
		}
	}
}

// Every resource on one message shares a MessageID, so without the index
// three photos in one send produce three objects with the same name.
func TestGeneratedMediaFilenamesAreDistinctWithinAMessage(t *testing.T) {
	lm := InboundMessage{MessageID: "om_abc123"}
	res := larkMediaResource{kind: channel.MsgTypeImage}
	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		name := mediaFilename(lm, res, DownloadedResourceStream{}, "image/png", i)
		seen[name]++
	}
	if len(seen) != 3 {
		t.Fatalf("three attachments produced %d distinct names (%v) — the later ones overwrite or confuse the earlier", len(seen), seen)
	}
}

// A real filename from the platform still wins over the generated one, index
// or not.
func TestProvidedFilenameStillWins(t *testing.T) {
	lm := InboundMessage{MessageID: "om_abc123"}
	res := larkMediaResource{kind: channel.MsgTypeFile, filename: "quarterly.pdf"}
	if got := mediaFilename(lm, res, DownloadedResourceStream{}, "application/pdf", 2); got != "quarterly.pdf" {
		t.Errorf("mediaFilename = %q, want the platform's own name", got)
	}
}

func TestProvidedAudioFilenameKeepsExistingExtension(t *testing.T) {
	lm := InboundMessage{MessageID: "om_audio"}
	res := larkMediaResource{kind: channel.MsgTypeAudio}
	downloaded := DownloadedResourceStream{Filename: "voice.ogg"}
	if got := mediaFilename(lm, res, downloaded, "audio/opus", 0); got != "voice.ogg" {
		t.Errorf("mediaFilename = %q, want the platform's existing extension preserved", got)
	}
}
