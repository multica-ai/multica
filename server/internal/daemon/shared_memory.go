package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	sharedMemoryDefaultRoot = "/Users/macmini16g256ssd/nexai/state/multica-memory"
	sharedMemoryTopN        = 8
	sharedMemoryMaxTokens   = 1400
	sharedMemorySearchLimit = 2 * time.Second
)

type sharedMemoryFact struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type sharedMemorySearchResult struct {
	Items []sharedMemoryFact `json:"items"`
}

// BuildSharedMemoryContext adds a bounded, task-relevant view of the local
// shared memory store. Search errors and timeouts are deliberately non-fatal;
// the daemon falls back to its dependency-free BM25-style scorer.
func BuildSharedMemoryContext(task Task) string {
	if !sharedMemoryEnabled() {
		return ""
	}
	root := strings.TrimSpace(os.Getenv("MULTICA_MEMORY_ROOT"))
	if root == "" {
		root = sharedMemoryDefaultRoot
	}
	query := sharedMemoryQuery(task)
	if query == "" {
		return ""
	}

	started := time.Now()
	var facts []sharedMemoryFact
	python := sharedMemoryPython()
	ctx, cancel := context.WithTimeout(context.Background(), sharedMemorySearchLimit)
	defer cancel()
	if script := strings.TrimSpace(os.Getenv("MULTICA_MEMORY_SEARCH_SCRIPT")); script != "" {
		if raw, err := exec.CommandContext(ctx, python, script, "--root", root, "--query", query, "--top-n", "8").Output(); err == nil {
			var result sharedMemorySearchResult
			if json.Unmarshal(raw, &result) == nil {
				facts = result.Items
			}
		}
	} else {
		script = filepath.Join(filepath.Dir(sharedMemoryDefaultRoot), "..", "scripts", "memory_search.py")
		if raw, err := exec.CommandContext(ctx, python, filepath.Clean(script), "--root", root, "--query", query, "--top-n", "8").Output(); err == nil {
			var result sharedMemorySearchResult
			if json.Unmarshal(raw, &result) == nil {
				facts = result.Items
			}
		}
	}
	if len(facts) == 0 {
		facts = searchSharedMemoryBM25(root, query, sharedMemoryTopN)
	}
	if time.Since(started) > sharedMemorySearchLimit {
		return ""
	}
	return renderSharedMemoryContext(facts)
}

func sharedMemoryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_SHARED_MEMORY"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func sharedMemoryPython() string {
	if python := strings.TrimSpace(os.Getenv("MULTICA_MEMORY_PYTHON")); python != "" {
		return python
	}
	local := filepath.Join(filepath.Dir(sharedMemoryDefaultRoot), ".venvs", "nex1848-memory", "bin", "python")
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		return local
	}
	return "python3"
}

func sharedMemoryQuery(task Task) string {
	// For a comment-triggered run, the comment is the user's immediate intent.
	// Including the full issue description here lets its repeated/background
	// terms drown out a short, specific question in BM25 fallback retrieval.
	if comment := strings.TrimSpace(task.TriggerCommentContent); comment != "" {
		return truncateMemoryQuery(comment, 12000)
	}
	parts := []string{
		task.ThreadName, task.IssueTitle, task.IssueDescription,
		task.ChatHistory, task.ChatMessage,
		task.AutopilotTitle, task.AutopilotDescription, task.QuickCreatePrompt,
		task.HandoffNote,
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		part = truncateMemoryQuery(part, max(0, 12000-b.Len()))
		b.WriteString(part)
		if b.Len() >= 12000 {
			break
		}
	}
	return b.String()
}

func truncateMemoryQuery(query string, maxBytes int) string {
	if len(query) <= maxBytes {
		return query
	}
	// Do not split a multi-byte rune when applying the query byte cap.
	query = query[:maxBytes]
	for !utf8.ValidString(query) {
		query = query[:len(query)-1]
	}
	return query
}

func searchSharedMemoryBM25(root, query string, limit int) []sharedMemoryFact {
	paths, _ := filepath.Glob(filepath.Join(root, "facts", "*.md"))
	paths = append(paths, filepath.Join(root, "MEMORY.md"), filepath.Join(root, "state", "current-state.json"))
	statePath := filepath.Join(root, "state", "current-state.json")
	if _, err := os.Stat(statePath); err == nil {
		paths = append(paths, statePath+"#kis.overseas_etp_enabled", statePath+"#kis.leverage_etp_education")
	}
	terms := memoryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	all := make([]sharedMemoryFact, 0, len(paths))
	for _, path := range paths {
		var content []byte
		var err error
		if strings.Contains(path, "#") {
			base, key, _ := strings.Cut(path, "#")
			stateBytes, readErr := os.ReadFile(base)
			if readErr != nil {
				continue
			}
			var state map[string]map[string]any
			if json.Unmarshal(stateBytes, &state) != nil || state[key] == nil {
				continue
			}
			label := "해외 ETP 신청 거래가능 로그인 상태"
			if key == "kis.leverage_etp_education" {
				label = "레버리지 ETF ETP 사전교육 이수 번호 키체인 등록 상태"
			}
			encoded, _ := json.Marshal(state[key])
			content = []byte(label + "\nkey: " + key + "\n" + string(encoded))
		} else {
			content, err = os.ReadFile(path)
		}
		if err != nil || len(content) == 0 {
			continue
		}
		text := string(content)
		tokens := memoryTerms(text)
		if len(tokens) == 0 {
			continue
		}
		freq := make(map[string]int, len(tokens))
		for _, token := range tokens {
			freq[token]++
		}
		score := 0.0
		for _, term := range terms {
			if n := freq[term]; n > 0 {
				score += 1 + float64(n)/(float64(n)+1.2*(0.25+0.75*float64(len(tokens))/300))
			}
		}
		if score > 0 {
			all = append(all, sharedMemoryFact{Path: path, Content: text, Score: score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if limit > sharedMemoryTopN {
		limit = sharedMemoryTopN
	}
	if limit > len(all) {
		limit = len(all)
	}
	return all[:limit]
}

func memoryTerms(text string) []string {
	var terms []string
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		w := strings.ToLower(string(word))
		if len([]rune(w)) > 1 {
			terms = append(terms, w)
		}
		// Character bigrams make Korean compound words searchable without an
		// external tokenizer and keep the fallback deterministic.
		if len(word) >= 3 && unicode.In(word[0], unicode.Hangul) {
			for i := 0; i+1 < len(word); i++ {
				terms = append(terms, string(word[i:i+2]))
			}
		}
		word = word[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return terms
}

func renderSharedMemoryContext(facts []sharedMemoryFact) string {
	if len(facts) == 0 {
		return ""
	}
	if len(facts) > sharedMemoryTopN {
		facts = facts[:sharedMemoryTopN]
	}
	var b strings.Builder
	b.WriteString("\n\nRelevant shared memory (reference context; verify current state from the cited source):\n")
	const maxBytes = sharedMemoryMaxTokens * 4
	for _, fact := range facts {
		content := strings.TrimSpace(fact.Content)
		if content == "" {
			continue
		}
		prefix := "\n- source: " + fact.Path + "\n  "
		const suffix = "\n"
		remaining := maxBytes - b.Len() - len(prefix) - len(suffix)
		if remaining <= 0 {
			continue
		}
		content = truncateMemoryContent(content, remaining)
		if content == "" {
			continue
		}
		fmtLine := prefix + strings.ReplaceAll(content, "\n", "\n  ") + suffix
		if b.Len()+len(fmtLine) > maxBytes {
			continue
		}
		b.WriteString(fmtLine)
	}
	return b.String()
}

// truncateMemoryContent bounds the rendered form, including the two-space
// indentation added after each newline. Binary search operates on source bytes;
// truncateUTF8 keeps every candidate at a valid UTF-8 boundary.
func truncateMemoryContent(content string, maxRenderedBytes int) string {
	low, high := 0, len(content)
	for low < high {
		mid := low + (high-low+1)/2
		candidate := truncateUTF8(content, mid)
		rendered := strings.ReplaceAll(candidate, "\n", "\n  ")
		if len(rendered) <= maxRenderedBytes {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return truncateUTF8(content, low)
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
