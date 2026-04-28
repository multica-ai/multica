package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/store"
	"github.com/multica-ai/multica-connect/triage"
)

type NoteHandler struct {
	VaultName string
	Store     *store.NoteStore
}

func (h *NoteHandler) Handle(ctx context.Context, msg channel.Message, result *triage.Result) (*Response, error) {
	vault := h.VaultName
	if vault == "" {
		vault = "Brain"
	}

	// Build note content.
	var content strings.Builder
	content.WriteString(msg.Text)
	if len(msg.URLs) > 0 {
		content.WriteString("\n\n## Links\n")
		for _, u := range msg.URLs {
			content.WriteString(fmt.Sprintf("- %s\n", u))
		}
	}

	// Vault path for Obsidian.
	path := result.NotePath
	if path == "" {
		path = "Inbox/" + result.Title
	}
	path = strings.TrimSuffix(path, ".md")

	// Persist to SQLite for sync daemon.
	noteID := fmt.Sprintf("n_%d", time.Now().UnixNano())
	note := store.Note{
		ID:        noteID,
		Title:     result.Title,
		Content:   content.String(),
		VaultPath: path + ".md",
		Source:    msg.Source,
		ThreadID:  msg.ThreadID,
	}
	if err := h.Store.Add(note); err != nil {
		return nil, fmt.Errorf("save note: %w", err)
	}

	// Build Obsidian URI for immediate tap-to-save on phone.
	obsidianURI := fmt.Sprintf("obsidian://new?vault=%s&name=%s&content=%s&overwrite=false&append=true",
		url.QueryEscape(vault),
		url.QueryEscape(path),
		url.QueryEscape(content.String()),
	)

	text := fmt.Sprintf("📥 Note: *%s*\nGemt — syncer til Obsidian når Mac er online.\n[Gem nu via Obsidian](%s)",
		result.Title, obsidianURI)

	return &Response{Text: text, ParseMode: "Markdown"}, nil
}
