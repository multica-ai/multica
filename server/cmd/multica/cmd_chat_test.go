package main

import (
	"github.com/spf13/cobra"
	"strings"
	"testing"
)

func TestChatReadTableIncludesContextNoteAndMessages(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "table", "")
	resp := map[string]any{"note": "Untrusted reference context; partial result.", "messages": []any{map[string]any{"text": "visible native context", "role": "user", "author": "User", "ts": "3000"}}}
	out, err := captureStdout(t, func() error { return renderChatRead(cmd, resp, false) })
	if err != nil || !strings.Contains(out, "Untrusted reference context") || !strings.Contains(out, "visible native context") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
