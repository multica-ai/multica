package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type providerTranscriptExtractor interface {
	ExtractTurns() ([]providerTranscriptTurn, bool)
}

type providerTranscriptEventExtractor interface {
	ExtractEvents() ([]providerTranscriptEvent, bool)
}

type providerTranscriptTurn struct {
	Key       string
	UserInput string
	Final     string
}

type providerTranscriptEvent struct {
	Key     string
	Source  string
	Type    string
	Tool    string
	Content string
	Input   map[string]any
	Output  string
	Comment bool
}

func newProviderTranscriptExtractor(cliName, cwd string, runStart time.Time, bootstrapPrompt string) providerTranscriptExtractor {
	switch strings.ToLower(strings.TrimSpace(cliName)) {
	case "codex":
		return &codexTranscriptExtractor{cwd: cwd, runStart: runStart, bootstrapPrompt: bootstrapPrompt}
	default:
		return nil
	}
}

func supportsProviderTranscript(cliName string) bool {
	switch strings.ToLower(strings.TrimSpace(cliName)) {
	case "codex":
		return true
	default:
		return false
	}
}

type codexTranscriptExtractor struct {
	mu              sync.Mutex
	cwd             string
	runStart        time.Time
	bootstrapPrompt string
	sessionPath     string
}

func (e *codexTranscriptExtractor) Extract(userPrompt string, turnStart time.Time) (string, bool) {
	path, ok := e.latestSessionFile(turnStart)
	if !ok {
		return "", false
	}
	return extractCodexAnswerFromJSONL(path, userPrompt)
}

func (e *codexTranscriptExtractor) ExtractUserInput(stdinPrompt string, turnStart time.Time) (string, bool) {
	path, ok := e.latestSessionFile(turnStart)
	if !ok {
		return "", false
	}
	return extractCodexUserInputFromJSONL(path, stdinPrompt)
}

func (e *codexTranscriptExtractor) ExtractTurns() ([]providerTranscriptTurn, bool) {
	path, ok := e.latestSessionFile(time.Time{})
	if !ok {
		return nil, false
	}
	return extractCodexTurnsFromJSONL(path), true
}

func (e *codexTranscriptExtractor) ExtractEvents() ([]providerTranscriptEvent, bool) {
	path, ok := e.latestSessionFile(time.Time{})
	if !ok {
		return nil, false
	}
	return extractCodexEventsFromJSONL(path), true
}

func (e *codexTranscriptExtractor) latestSessionFile(turnStart time.Time) (string, bool) {
	e.mu.Lock()
	if e.sessionPath != "" {
		path := e.sessionPath
		e.mu.Unlock()
		return path, true
	}
	e.mu.Unlock()

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
	var bestBoundPath string
	var bestBoundMod time.Time
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
			if strings.TrimSpace(e.bootstrapPrompt) != "" && codexSessionContainsUserMessage(path, e.bootstrapPrompt) {
				if bestBoundPath == "" || info.ModTime().After(bestBoundMod) {
					bestBoundPath = path
					bestBoundMod = info.ModTime()
				}
			}
			return nil
		})
	}
	if bestBoundPath != "" {
		e.mu.Lock()
		e.sessionPath = bestBoundPath
		e.mu.Unlock()
		return bestBoundPath, true
	}
	if strings.TrimSpace(e.bootstrapPrompt) != "" {
		return "", false
	}
	return bestPath, bestPath != ""
}

func codexSessionContainsUserMessage(path, prompt string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	want := normalizeProviderText(prompt)
	if want == "" {
		return false
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		payload, ok := codexEventPayload(scanner.Bytes())
		if !ok || stringValue(payload["type"]) != "user_message" {
			continue
		}
		if normalizeProviderText(codexPayloadText(payload)) == want {
			return true
		}
	}
	return false
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
		turn       codexAnswerCandidates
		lastAnswer string
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
			if collecting {
				if answer, ok := turn.answer(); ok {
					lastAnswer = answer
				}
			}
			collecting = promptMatches(codexPayloadText(payload), userPrompt)
			if collecting {
				matched = true
				turn = codexAnswerCandidates{}
				lastAnswer = ""
			}
		case "agent_message":
			if collecting {
				if text := strings.TrimSpace(codexPayloadText(payload)); text != "" {
					switch stringValue(payload["phase"]) {
					case "final_answer":
						turn.finalAnswers = append(turn.finalAnswers, text)
					case "":
						turn.legacyAnswers = append(turn.legacyAnswers, text)
					}
				}
			}
		case "task_complete":
			if collecting {
				if text := strings.TrimSpace(codexTaskCompleteLastAgentMessage(payload)); text != "" {
					turn.taskCompleteAnswer = text
				}
			}
		}
	}
	if matched {
		if answer, ok := turn.answer(); ok {
			return answer, true
		}
	}
	if lastAnswer != "" {
		return lastAnswer, true
	}
	return "", false
}

func extractCodexUserInputFromJSONL(path, stdinPrompt string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	var last string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		payload, ok := codexEventPayload(scanner.Bytes())
		if !ok || stringValue(payload["type"]) != "user_message" {
			continue
		}
		text := strings.TrimSpace(codexPayloadText(payload))
		if promptMatches(text, stdinPrompt) {
			last = text
		}
	}
	if last == "" {
		return "", false
	}
	return last, true
}

func extractCodexTurnsFromJSONL(path string) []providerTranscriptTurn {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var (
		turns      []providerTranscriptTurn
		collecting bool
		key        string
		userInput  string
		candidates codexAnswerCandidates
		lineNo     int
	)
	flush := func() {
		if !collecting {
			return
		}
		turn := providerTranscriptTurn{Key: key, UserInput: strings.TrimSpace(userInput)}
		if answer, ok := candidates.answer(); ok {
			turn.Final = strings.TrimSpace(answer)
		}
		if turn.UserInput != "" {
			turns = append(turns, turn)
		}
		collecting = false
		key = ""
		userInput = ""
		candidates = codexAnswerCandidates{}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lineNo++
		payload, ok := codexEventPayload(scanner.Bytes())
		if !ok {
			continue
		}
		switch stringValue(payload["type"]) {
		case "user_message":
			flush()
			text := strings.TrimSpace(codexPayloadText(payload))
			if text == "" {
				continue
			}
			collecting = true
			key = fmt.Sprintf("%s:%d", path, lineNo)
			userInput = text
			candidates = codexAnswerCandidates{}
		case "agent_message":
			if collecting {
				if text := strings.TrimSpace(codexPayloadText(payload)); text != "" {
					switch stringValue(payload["phase"]) {
					case "final_answer":
						candidates.finalAnswers = append(candidates.finalAnswers, text)
					case "":
						candidates.legacyAnswers = append(candidates.legacyAnswers, text)
					}
				}
			}
		case "task_complete":
			if collecting {
				if text := strings.TrimSpace(codexTaskCompleteLastAgentMessage(payload)); text != "" {
					candidates.taskCompleteAnswer = text
				}
			}
		}
	}
	flush()
	return turns
}

func extractCodexEventsFromJSONL(path string) []providerTranscriptEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []providerTranscriptEvent
	commentableTurn := false
	lastFinal := ""
	lineNo := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lineNo++
		payload, ok := codexEventPayload(scanner.Bytes())
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s:%d", path, lineNo)
		switch stringValue(payload["type"]) {
		case "user_message":
			text := strings.TrimSpace(codexPayloadText(payload))
			if text == "" {
				continue
			}
			commentableTurn = !isSlashInput(text)
			lastFinal = ""
			events = append(events, providerTranscriptEvent{
				Key:     key + ":user",
				Source:  "codex",
				Type:    "user_input",
				Content: text,
				Comment: commentableTurn,
			})
		case "agent_message":
			text := strings.TrimSpace(codexPayloadText(payload))
			if text == "" {
				continue
			}
			if stringValue(payload["phase"]) == "final_answer" {
				events = append(events, providerTranscriptEvent{
					Key:     key + ":final",
					Source:  "codex",
					Type:    "final",
					Content: text,
					Comment: commentableTurn,
				})
				lastFinal = text
			} else {
				events = append(events, providerTranscriptEvent{
					Key:     key + ":text",
					Source:  "codex",
					Type:    "text",
					Content: text,
				})
			}
		case "function_call":
			events = append(events, providerTranscriptEvent{
				Key:    key + ":tool_use",
				Source: "codex",
				Type:   "tool_use",
				Tool:   firstString(payload, "name", "tool", "tool_name"),
				Input:  codexFunctionCallInput(payload),
			})
		case "function_call_output":
			output := strings.TrimSpace(codexPayloadText(payload))
			if output == "" {
				output = stringValue(payload["output"])
			}
			events = append(events, providerTranscriptEvent{
				Key:    key + ":tool_result",
				Source: "codex",
				Type:   "tool_result",
				Tool:   firstString(payload, "name", "tool", "tool_name"),
				Output: output,
			})
		case "task_complete":
			text := strings.TrimSpace(codexTaskCompleteLastAgentMessage(payload))
			if text != "" && text != lastFinal {
				events = append(events, providerTranscriptEvent{
					Key:     key + ":final",
					Source:  "codex",
					Type:    "final",
					Content: text,
					Comment: commentableTurn,
				})
			}
		case "error":
			text := strings.TrimSpace(codexPayloadText(payload))
			if text != "" {
				events = append(events, providerTranscriptEvent{
					Key:     key + ":error",
					Source:  "codex",
					Type:    "error",
					Content: text,
				})
			}
		}
	}
	return events
}

func codexFunctionCallInput(payload map[string]any) map[string]any {
	input := make(map[string]any)
	if callID := stringValue(payload["call_id"]); callID != "" {
		input["call_id"] = callID
	}
	if args := stringValue(payload["arguments"]); args != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(args), &parsed); err == nil {
			input["arguments"] = parsed
		} else {
			input["arguments"] = args
		}
	}
	if len(input) == 0 {
		return nil
	}
	return input
}

type codexAnswerCandidates struct {
	taskCompleteAnswer string
	finalAnswers       []string
	legacyAnswers      []string
}

func (c codexAnswerCandidates) answer() (string, bool) {
	if answer := strings.TrimSpace(c.taskCompleteAnswer); answer != "" {
		return answer, true
	}
	if answer := joinProviderMessages(c.finalAnswers); answer != "" {
		return answer, true
	}
	if answer := joinProviderMessages(c.legacyAnswers); answer != "" {
		return answer, true
	}
	return "", false
}

func codexTaskCompleteLastAgentMessage(payload map[string]any) string {
	if text := stringValue(payload["last_agent_message"]); text != "" {
		return text
	}
	if msg, ok := payload["last_agent_message"].(map[string]any); ok {
		return codexPayloadText(msg)
	}
	if result, ok := payload["result"].(map[string]any); ok {
		if text := stringValue(result["last_agent_message"]); text != "" {
			return text
		}
		if msg, ok := result["last_agent_message"].(map[string]any); ok {
			return codexPayloadText(msg)
		}
	}
	return ""
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
	candidates := canonicalPromptVariants(candidate)
	prompts := canonicalPromptVariants(prompt)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, p := range prompts {
			if p == "" {
				continue
			}
			if c == p {
				return true
			}
			if promptCanMatchAsSubstring(p) && strings.Contains(c, p) {
				return true
			}
		}
	}
	return false
}

func normalizeProviderText(s string) string {
	return normalizeCapturedUserText(s)
}

func canonicalPromptVariants(s string) []string {
	base := normalizeProviderText(s)
	if base == "" {
		return nil
	}
	variants := []string{base}
	if stripped := stripLeadingPathTokens(base); stripped != "" && stripped != base {
		variants = append(variants, stripped)
	}
	if stripped := stripImagePlaceholders(base); stripped != "" && stripped != base {
		variants = append(variants, stripped)
	}
	return uniqueStrings(variants)
}

func stripLeadingPathTokens(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	for len(fields) > 0 && looksLikePathToken(fields[0]) {
		fields = fields[1:]
	}
	return normalizeProviderText(strings.Join(fields, " "))
}

func stripImagePlaceholders(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "[Image" && i+1 < len(fields) && strings.HasSuffix(fields[i+1], "]") {
			i++
			continue
		}
		out = append(out, fields[i])
	}
	return normalizeProviderText(strings.Join(out, " "))
}

func promptCanMatchAsSubstring(s string) bool {
	return len([]rune(s)) >= 20 || containsLikelyFileName(s)
}

func containsLikelyFileName(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		fields = []string{s}
	}
	for _, field := range fields {
		field = strings.Trim(field, "`'\"")
		dot := strings.LastIndex(field, ".")
		if dot <= 0 || dot+1 >= len(field) {
			continue
		}
		if hasAlphaNumAfterDot(field[dot+1:]) {
			return true
		}
	}
	return false
}

func hasAlphaNumAfterDot(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return true
		}
		break
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
