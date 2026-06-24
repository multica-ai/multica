package recall

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultMaxHits        = 5
	defaultMaxBundleBytes = 12 * 1024
	defaultMaxIndexAge    = 7 * 24 * time.Hour
	maxExcerptBytes       = 2048
	maxNoteReadBytes      = 64 * 1024
)

type scoredEntry struct {
	entry Entry
	score float64
}

func Run(ctx context.Context, options Options, query Query) Result {
	options = normalizeOptions(options)
	result := Result{
		Status: StatusNoHit, Query: query.Text(), IndexVersion: CurrentIndexVersion,
		ByteBudget: options.MaxBundleBytes, Hits: []Hit{},
	}
	fitResultWithinBudget(&result)
	if strings.TrimSpace(options.VaultRoot) == "" {
		result.Reason = "vault_not_configured"
		fitResultWithinBudget(&result)
		return result
	}

	index, status, reason := loadIndex(options)
	if status != "" {
		result.Status = status
		result.Reason = reason
		fitResultWithinBudget(&result)
		return result
	}
	result.IndexVersion = index.IndexVersion

	queryTokens := tokenize(result.Query)
	if len(queryTokens) == 0 {
		result.Reason = "query_empty"
		fitResultWithinBudget(&result)
		return result
	}

	candidates := make([]scoredEntry, 0, len(index.Entries))
	for _, entry := range index.Entries {
		if err := ctx.Err(); err != nil {
			result.Status = StatusControlledError
			result.Reason = "context_cancelled"
			fitResultWithinBudget(&result)
			return result
		}
		if !isAllowedEntry(entry) {
			result.SkippedFiles = append(result.SkippedFiles, entry.Path)
			continue
		}
		score := scoreEntry(queryTokens, entry)
		if score > 0 {
			candidates = append(candidates, scoredEntry{entry: entry, score: score})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].entry.MTime != candidates[j].entry.MTime {
			return candidates[i].entry.MTime > candidates[j].entry.MTime
		}
		return candidates[i].entry.Path < candidates[j].entry.Path
	})

	for _, candidate := range candidates {
		if len(result.Hits) >= options.MaxHits {
			break
		}
		excerpt, err := readExcerpt(options.VaultRoot, candidate.entry.Path)
		if err != nil {
			result.SkippedFiles = append(result.SkippedFiles, candidate.entry.Path)
			continue
		}
		hit := Hit{
			Path: candidate.entry.Path, Recency: candidate.entry.MTime,
			Relevance: candidate.score, Excerpt: excerpt,
		}
		if !appendWithinBudget(&result, hit, options.MaxBundleBytes) {
			break
		}
	}

	result.HitCount = len(result.Hits)
	if result.HitCount > 0 {
		result.Status = StatusHit
		result.Reason = ""
	} else {
		result.Reason = "no_relevant_notes"
	}
	fitResultWithinBudget(&result)
	return result
}

func normalizeOptions(options Options) Options {
	if options.MaxHits <= 0 || options.MaxHits > defaultMaxHits {
		options.MaxHits = defaultMaxHits
	}
	if options.MaxBundleBytes < 512 {
		options.MaxBundleBytes = defaultMaxBundleBytes
	}
	if options.MaxIndexAge <= 0 {
		options.MaxIndexAge = defaultMaxIndexAge
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func fitResultWithinBudget(result *Result) {
	if len([]byte(result.Render())) <= result.ByteBudget {
		return
	}
	runes := []rune(result.Query)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		mid := low + (high-low)/2
		candidate := ""
		if mid > 0 {
			candidate = strings.TrimSpace(string(runes[:mid])) + "…[truncated]"
		}
		result.Query = candidate
		if len([]byte(result.Render())) <= result.ByteBudget {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	result.Query = best
	result.Render()
}

func loadIndex(options Options) (Index, Status, string) {
	indexPath := filepath.Join(options.VaultRoot, "ops", "recall-index.json")
	data, err := os.ReadFile(indexPath)
	if errors.Is(err, os.ErrNotExist) {
		return Index{}, StatusNoHit, "index_missing"
	}
	if err != nil {
		return Index{}, StatusControlledError, "index_unreadable"
	}
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, StatusControlledError, "index_invalid"
	}
	if index.IndexVersion != CurrentIndexVersion {
		return Index{}, StatusNoHit, "index_version_incompatible"
	}
	generatedAt, err := time.Parse(time.RFC3339, index.GeneratedAt)
	if err != nil {
		return Index{}, StatusControlledError, "index_invalid"
	}
	if options.Now().Sub(generatedAt) > options.MaxIndexAge {
		return Index{}, StatusNoHit, "index_stale"
	}
	return index, "", ""
}

func isAllowedEntry(entry Entry) bool {
	clean := path.Clean(strings.ReplaceAll(entry.Path, "\\", "/"))
	if clean != entry.Path || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return false
	}
	if !strings.HasPrefix(clean, "notes/") || !strings.EqualFold(path.Ext(clean), ".md") {
		return false
	}
	base := path.Base(clean)
	lower := strings.ToLower(base)
	return !strings.EqualFold(base, "MEMORY.md") &&
		!strings.EqualFold(base, "agent-memory-index.md") &&
		!strings.HasPrefix(lower, "unbenannt") &&
		!strings.Contains(lower, "sync-conflict")
}

func tokenize(value string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var current strings.Builder
	flush := func() {
		if current.Len() < 2 {
			current.Reset()
			return
		}
		tokens[current.String()] = struct{}{}
		current.Reset()
	}
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func scoreEntry(queryTokens map[string]struct{}, entry Entry) float64 {
	weighted := []struct {
		value  string
		weight float64
	}{
		{value: entry.Title, weight: 4},
		{value: strings.Join(entry.Tags, " "), weight: 3},
		{value: entry.Summary, weight: 2},
	}
	var score float64
	for _, field := range weighted {
		for token := range tokenize(field.value) {
			if _, ok := queryTokens[token]; ok {
				score += field.weight
			}
		}
	}
	return score
}

func readExcerpt(vaultRoot, relativePath string) (string, error) {
	fullPath := filepath.Join(vaultRoot, filepath.FromSlash(relativePath))
	rootAbs, err := filepath.Abs(vaultRoot)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	file, err := os.Open(pathAbs)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, maxNoteReadBytes))
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	excerpt := strings.TrimSpace(string(data))
	if excerpt == "" {
		return "", io.EOF
	}
	return truncateUTF8(excerpt, maxExcerptBytes), nil
}

func appendWithinBudget(result *Result, hit Hit, budget int) bool {
	result.Hits = append(result.Hits, hit)
	result.HitCount = len(result.Hits)
	if len([]byte(result.Render())) <= budget {
		return true
	}

	runes := []rune(hit.Excerpt)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		mid := low + (high-low)/2
		candidate := ""
		if mid > 0 {
			candidate = strings.TrimSpace(string(runes[:mid])) + "…[truncated]"
		}
		result.Hits[len(result.Hits)-1].Excerpt = candidate
		if len([]byte(result.Render())) <= budget {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best != "" {
		result.Hits[len(result.Hits)-1].Excerpt = best
		result.Render()
		return true
	}
	result.Hits = result.Hits[:len(result.Hits)-1]
	result.HitCount = len(result.Hits)
	result.Render()
	return false
}

func truncateUTF8(value string, maxBytes int) string {
	if len([]byte(value)) <= maxBytes {
		return value
	}
	marker := "…[truncated]"
	limit := maxBytes - len([]byte(marker))
	if limit <= 0 {
		return ""
	}
	data := []byte(value)
	for limit > 0 && !utf8.Valid(data[:limit]) {
		limit--
	}
	return strings.TrimSpace(string(data[:limit])) + marker
}
