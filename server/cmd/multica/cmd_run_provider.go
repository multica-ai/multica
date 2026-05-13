package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type providerTranscriptExtractor interface {
	Extract(userPrompt string, turnStart time.Time) (string, bool)
}

func newProviderTranscriptExtractor(cliName, cwd string, runStart time.Time) providerTranscriptExtractor {
	switch strings.ToLower(strings.TrimSpace(cliName)) {
	case "codex":
		return codexTranscriptExtractor{cwd: cwd, runStart: runStart}
	default:
		return nil
	}
}

type codexTranscriptExtractor struct {
	cwd      string
	runStart time.Time
}

func (e codexTranscriptExtractor) Extract(userPrompt string, turnStart time.Time) (string, bool) {
	path, ok := e.latestSessionFile(turnStart)
	if !ok {
		return "", false
	}
	return extractCodexAnswerFromJSONL(path, userPrompt)
}

func (e codexTranscriptExtractor) latestSessionFile(turnStart time.Time) (string, bool) {
	roots := codexSessionRoots()
	if len(roots) == 0 {
		return "", false
	}
	cutoff := e.runStart
	if !turnStart.IsZero() && turnStart.Before(cutoff) {
		cutoff = turnStart
	}
	cutoff = cutoff.Add(-2 * time.Second)

	var bestPath string
	var bestMod time.Time
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(cutoff) {
				return nil
			}
			if bestPath == "" || info.ModTime().After(bestMod) {
				bestPath = path
				bestMod = info.ModTime()
			}
			return nil
		})
	}
	return bestPath, bestPath != ""
}

func codexSessionRoots() []string {
	var roots []string
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		roots = append(roots, filepath.Join(home, "sessions"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		roots = append(roots, filepath.Join(home, ".codex", "sessions"))
	}
	return roots
}

func extractCodexAnswerFromJSONL(path, userPrompt string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	var (
		collecting bool
		matched    bool
		answers    []string
		lastAnswer []string
	)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		payload, ok := codexEventPayload(scanner.Bytes())
		if !ok {
			continue
		}
		switch stringValue(payload["type"]) {
		case "user_message":
			if collecting && len(answers) > 0 {
				lastAnswer = append(lastAnswer[:0], answers...)
			}
			collecting = promptMatches(codexPayloadText(payload), userPrompt)
			if collecting {
				matched = true
				answers = answers[:0]
				lastAnswer = nil
			}
		case "agent_message":
			if collecting {
				if text := strings.TrimSpace(codexPayloadText(payload)); text != "" {
					answers = append(answers, text)
				}
			}
		}
	}
	if matched && len(answers) > 0 {
		return joinProviderMessages(answers), true
	}
	if len(lastAnswer) > 0 {
		return joinProviderMessages(lastAnswer), true
	}
	return "", false
}

func codexEventPayload(line []byte) (map[string]any, bool) {
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		return nil, false
	}
	if stringValue(obj["type"]) == "event_msg" {
		if payload, ok := obj["payload"].(map[string]any); ok {
			return payload, true
		}
		return nil, false
	}
	if _, ok := obj["payload"]; ok {
		if payload, ok := obj["payload"].(map[string]any); ok {
			return payload, true
		}
	}
	return obj, true
}

func codexPayloadText(payload map[string]any) string {
	if text := firstString(payload, "message", "text", "content"); text != "" {
		return text
	}
	if content, ok := payload["content"].([]any); ok {
		var parts []string
		for _, item := range content {
			switch v := item.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if text := firstString(v, "text", "message", "content"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func promptMatches(candidate, prompt string) bool {
	candidate = normalizeProviderText(candidate)
	prompt = normalizeProviderText(prompt)
	if candidate == "" || prompt == "" {
		return false
	}
	if candidate == prompt {
		return true
	}
	return len([]rune(prompt)) >= 20 && strings.Contains(candidate, prompt)
}

func normalizeProviderText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func joinProviderMessages(messages []string) string {
	var out []string
	for _, msg := range messages {
		msg = strings.TrimSpace(msg)
		if msg != "" {
			out = append(out, msg)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n\n"))
}
