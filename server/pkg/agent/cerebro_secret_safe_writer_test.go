package agent

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretSafeDiagnosticWriterRedactsAcrossChunkBoundaries(t *testing.T) {
	t.Parallel()

	var sink bytes.Buffer
	writer := newSecretSafeDiagnosticWriter(&sink)
	_, _ = writer.Write([]byte("provider failed refresh_token=synthetic-split-"))
	_, _ = writer.Write([]byte("refresh-value\n"))
	writer.Flush()

	if strings.Contains(sink.String(), "synthetic-split-") || strings.Contains(sink.String(), "refresh-value") {
		t.Fatalf("credential fragment reached diagnostic sink: %s", sink.String())
	}
	if !strings.Contains(sink.String(), "[REDACTED OAUTH REFRESH]") {
		t.Fatalf("redaction marker missing: %s", sink.String())
	}
}
