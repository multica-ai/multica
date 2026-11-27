package execenv

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CodexContextUsage is the context size of a Codex thread's latest model
// request, as recorded by the last token_count event in its rollout.
type CodexContextUsage struct {
	// InputTokens is the latest request's input, cached tokens included: what
	// the thread carries into its next turn.
	InputTokens int64
	// ContextWindow is the model's context window as Codex reported it.
	ContextWindow int64
}

const (
	codexUsageReadChunk = 64 << 10
	// codexUsageScanLimit bounds how far back the reader looks. token_count
	// events follow every model request, so the latest one is normally in the
	// last few KiB; a rollout whose tail has none is left alone.
	codexUsageScanLimit = 16 << 20
)

// CodexResumeContextUsage reads the latest token_count event from
// sessionID's rollout in codexHome. ok is false when there is no plain rollout,
// it cannot be read, or its tail has no usable token_count event; callers
// then leave the resume alone.
func CodexResumeContextUsage(codexHome, sessionID string) (usage CodexContextUsage, ok bool) {
	if codexHome == "" || sessionID == "" {
		return CodexContextUsage{}, false
	}
	path := latestPlainCodexRollout(findCodexRollouts(filepath.Join(codexHome, "sessions"), sessionID))
	if path == "" {
		return CodexContextUsage{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return CodexContextUsage{}, false
	}
	defer f.Close()
	return lastCodexTokenCount(f)
}

// latestPlainCodexRollout picks the most recently written uncompressed
// rollout. Compressed (.jsonl.zst) rollouts are not read here.
func latestPlainCodexRollout(paths []string) string {
	var best string
	var bestMod int64
	for _, p := range paths {
		if !strings.HasSuffix(p, ".jsonl") {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if mod := info.ModTime().UnixNano(); best == "" || mod > bestMod {
			best, bestMod = p, mod
		}
	}
	return best
}

// lastCodexTokenCount reads the file backwards in chunks and returns the first
// complete token_count line it meets, so a normal check touches only the tail.
func lastCodexTokenCount(f *os.File) (CodexContextUsage, bool) {
	info, err := f.Stat()
	if err != nil {
		return CodexContextUsage{}, false
	}
	end := info.Size()
	var carry []byte // bytes after the last newline seen, not yet a full line
	scanned := int64(0)
	for end > 0 && scanned < codexUsageScanLimit {
		size := int64(codexUsageReadChunk)
		if size > end {
			size = end
		}
		start := end - size
		chunk := make([]byte, size)
		if _, err := f.ReadAt(chunk, start); err != nil && err != io.EOF {
			return CodexContextUsage{}, false
		}
		scanned += size
		end = start
		buf := append(chunk, carry...)
		// Every line except the first in buf is complete. The first may
		// continue in the previous chunk unless we are at the file start.
		for {
			i := bytes.LastIndexByte(buf, '\n')
			if i < 0 {
				break
			}
			if usage, done, ok := classifyCodexRolloutLine(buf[i+1:]); done {
				return usage, ok
			}
			buf = buf[:i]
		}
		if end == 0 {
			usage, _, ok := classifyCodexRolloutLine(buf)
			return usage, ok
		}
		carry = buf
	}
	return CodexContextUsage{}, false
}

// classifyCodexRolloutLine reports done when the backwards scan can stop: at
// a usable token_count (ok) or at a compaction newer than any token_count
// (not ok: the last recorded size predates the compaction and overstates the
// thread).
func classifyCodexRolloutLine(line []byte) (usage CodexContextUsage, done, ok bool) {
	if bytes.Contains(line, []byte(`"compacted"`)) {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(bytes.TrimSpace(line), &head) == nil && head.Type == "compacted" {
			return CodexContextUsage{}, true, false
		}
	}
	usage, ok = parseCodexTokenCountLine(line)
	return usage, ok, ok
}

func parseCodexTokenCountLine(line []byte) (CodexContextUsage, bool) {
	if !bytes.Contains(line, []byte(`"token_count"`)) {
		return CodexContextUsage{}, false
	}
	var entry struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Info *struct {
				LastTokenUsage *struct {
					InputTokens int64 `json:"input_tokens"`
				} `json:"last_token_usage"`
				ModelContextWindow int64 `json:"model_context_window"`
			} `json:"info"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(line), &entry); err != nil {
		return CodexContextUsage{}, false
	}
	if entry.Type != "event_msg" || entry.Payload.Type != "token_count" ||
		entry.Payload.Info == nil || entry.Payload.Info.LastTokenUsage == nil {
		return CodexContextUsage{}, false
	}
	return CodexContextUsage{
		InputTokens:   entry.Payload.Info.LastTokenUsage.InputTokens,
		ContextWindow: entry.Payload.Info.ModelContextWindow,
	}, true
}
