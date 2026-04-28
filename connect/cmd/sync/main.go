package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Note mirrors store.Note for JSON decoding.
type Note struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	VaultPath string `json:"vault_path"`
	CreatedAt string `json:"created_at"`
}

func main() {
	connectURL := envOrDefault("CONNECT_URL", "http://localhost:8190")
	obsidianURL := envOrDefault("OBSIDIAN_REST_URL", "http://localhost:27123")
	obsidianToken := os.Getenv("OBSIDIAN_REST_TOKEN")
	pollInterval := 30 * time.Second

	if v := os.Getenv("SYNC_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			pollInterval = d
		}
	}

	slog.Info("connect-sync starting",
		"connect_url", connectURL,
		"obsidian_url", obsidianURL,
		"poll_interval", pollInterval,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down")
		cancel()
	}()

	// Run once immediately, then poll.
	syncNotes(connectURL, obsidianURL, obsidianToken)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncNotes(connectURL, obsidianURL, obsidianToken)
		}
	}
}

func syncNotes(connectURL, obsidianURL, obsidianToken string) {
	// Pull unsynced notes from connect.
	resp, err := http.Get(connectURL + "/api/notes/unsynced")
	if err != nil {
		slog.Error("failed to fetch unsynced notes", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("fetch notes failed", "status", resp.StatusCode, "body", string(body))
		return
	}

	var result struct {
		Notes []Note `json:"notes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("decode notes response", "error", err)
		return
	}

	if len(result.Notes) == 0 {
		return
	}

	slog.Info("syncing notes to Obsidian", "count", len(result.Notes))

	for _, note := range result.Notes {
		if err := writeToObsidian(obsidianURL, obsidianToken, note); err != nil {
			slog.Error("failed to write note to Obsidian",
				"note_id", note.ID,
				"title", note.Title,
				"error", err,
			)
			continue
		}

		// Mark as synced.
		req, _ := http.NewRequest("POST", connectURL+"/api/notes/"+note.ID+"/synced", nil)
		markResp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("failed to mark note synced", "note_id", note.ID, "error", err)
			continue
		}
		markResp.Body.Close()

		slog.Info("synced note", "note_id", note.ID, "title", note.Title, "path", note.VaultPath)
	}
}

func writeToObsidian(baseURL, token string, note Note) error {
	// Obsidian Local REST API: PUT /vault/{path}
	// Creates or appends to a note.
	path := note.VaultPath
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}

	// Build markdown content with frontmatter.
	var content strings.Builder
	content.WriteString(fmt.Sprintf("---\nsource: multica-connect\ncreated: %s\n---\n\n", note.CreatedAt))
	content.WriteString(note.Content)
	content.WriteString("\n")

	url := fmt.Sprintf("%s/vault/%s", baseURL, path)
	req, err := http.NewRequest("PUT", url, bytes.NewBufferString(content.String()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "text/markdown")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("obsidian REST request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("obsidian API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
