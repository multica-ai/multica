package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	ParserSchemaVersion   = "knowledge-document-v1"
	GraphSchemaVersion    = "knowledge-graph-v1"
	ParserVersion         = "multica-parser-go-v1"
	ChunkerVersion        = "knowledge-chunker-v1"
	TokenizerVersion      = "jieba-search-v1"
	EmbeddingInputVersion = "document-title-path-text-v1"
	RRFConstant           = 60
	MaxVectorDimension    = 4096
)

var (
	ErrUnsupportedFormat = errors.New("unsupported knowledge document format")
	ErrInvalidVector     = errors.New("invalid embedding vector")
	ErrQuoteNotFound     = errors.New("evidence quote is not present in source text")
	ErrAmbiguousQuote    = errors.New("evidence quote occurs more than once")
	ErrInvalidURL        = errors.New("invalid source URL")
)

// NormalizeText is the intentionally small normalization used for matching
// and identity. It does not remove model numbers, years, punctuation, or
// corporate suffixes: those can be semantically important.
func NormalizeText(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		if r < utf8.RuneSelf {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func NormalizeAlias(s string) string { return NormalizeText(s) }

// NormalizeURL implements the documented URL deduplication boundary. Query
// parameters remain because they can select materially different content.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return "", ErrInvalidURL
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", ErrInvalidURL
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		if u.Scheme == "http" {
			u.Host = strings.TrimSuffix(u.Host, ":80")
		} else {
			u.Host = strings.TrimSuffix(u.Host, ":443")
		}
	}
	u.Fragment = ""
	return u.String(), nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func HashText(text string) string { return HashBytes([]byte(text)) }

// EstimateTokens is a conservative, deterministic estimate used for chunk
// sizing. Search tokenization is a separate fixed contract and must not be
// confused with a provider's input-token accounting.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	n := utf8.RuneCountInString(text)
	if n < 4 {
		return 1
	}
	return (n + 3) / 4
}

// SearchTokens is the dependency-free fallback for the parser service's
// pinned Jieba search tokenizer. The production parser supplies longer Chinese
// dictionary segments; this fallback keeps single Han characters and complete
// Latin/number identifiers searchable when the parser is not configured.
func SearchTokens(text string) []string {
	text = norm.NFKC.String(text)
	tokens := make([]string, 0)
	seen := make(map[string]struct{})
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		token := strings.ToLower(word.String())
		if _, ok := seen[token]; !ok {
			tokens = append(tokens, token)
			seen[token] = struct{}{}
		}
		word.Reset()
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			token := string(r)
			if _, ok := seen[token]; !ok {
				tokens = append(tokens, token)
				seen[token] = struct{}{}
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
			word.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return tokens
}

// VectorLiteral validates and formats a vector for pgvector's text input.
// Validation is performed before it reaches SQL so NaN, infinity, zero
// vectors, and accidental dimension changes cannot be persisted.
func VectorLiteral(values []float64, expectedDimension int) (string, error) {
	if len(values) == 0 || len(values) > MaxVectorDimension {
		return "", ErrInvalidVector
	}
	if expectedDimension != 0 && len(values) != expectedDimension {
		return "", fmt.Errorf("%w: dimension %d, expected %d", ErrInvalidVector, len(values), expectedDimension)
	}
	normSquared := 0.0
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", ErrInvalidVector
		}
		normSquared += value * value
	}
	if normSquared == 0 || math.IsInf(normSquared, 0) {
		return "", ErrInvalidVector
	}
	// Store normalized vectors so cosine distance is stable across providers
	// and the database query does not need a second normalization operation.
	n := math.Sqrt(normSquared)
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.FormatFloat(value/n, 'g', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func ParseVectorLiteral(literal string) ([]float64, error) {
	literal = strings.TrimSpace(literal)
	if len(literal) < 2 || literal[0] != '[' || literal[len(literal)-1] != ']' {
		return nil, ErrInvalidVector
	}
	var values []float64
	if err := json.Unmarshal([]byte("["+literal[1:len(literal)-1]+"]"), &values); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidVector, err)
	}
	return values, nil
}

// EmbeddingFingerprint changes whenever any part of the vector space changes.
// The API key is intentionally absent.
func EmbeddingFingerprint(providerID, model string, dimension int, inputTemplate, normalization string) string {
	payload := struct {
		ProviderID    string `json:"provider_id"`
		Model         string `json:"model"`
		Dimension     int    `json:"dimension"`
		InputTemplate string `json:"input_template"`
		Normalization string `json:"normalization"`
	}{providerID, model, dimension, inputTemplate, normalization}
	data, _ := json.Marshal(payload)
	return HashBytes(data)
}

// EntityIdentityKey deliberately keeps document scope unless a stable
// identifier or confirmed alias is available. Names alone cannot merge people,
// organisations, products, or events safely.
func EntityIdentityKey(entityType, normalizedName, stableIdentifier, documentID, qualifier string, confirmedAlias bool) string {
	entityType = NormalizeText(entityType)
	normalizedName = NormalizeText(normalizedName)
	stableIdentifier = NormalizeText(stableIdentifier)
	qualifier = NormalizeText(qualifier)
	identity := entityType + "|"
	switch {
	case stableIdentifier != "":
		identity += "stable:" + stableIdentifier
	case confirmedAlias:
		identity += "alias:" + normalizedName
	case entityType == "concept" && qualifier == "":
		identity += "concept:" + normalizedName
	default:
		identity += "document:" + documentID + "|name:" + normalizedName + "|qualifier:" + qualifier
	}
	return HashText(identity)
}

func RelationIdentityKey(sourceID, targetID, predicate string, qualifier map[string]any) string {
	if predicate == "related_to" && sourceID > targetID {
		sourceID, targetID = targetID, sourceID
	}
	qualifierJSON, _ := json.Marshal(qualifier)
	return HashText(sourceID + "|" + targetID + "|" + NormalizeText(predicate) + "|" + string(qualifierJSON))
}

type RankedChunk struct {
	ChunkID string
	Rank    int
	Result  SearchResult
}

// FuseRRF combines channel ranks, not incomparable raw scores. Ties are
// deterministic, which keeps query responses stable and makes fixtures useful.
func FuseRRF(keyword, vector []RankedChunk, limit int) []SearchResult {
	type fused struct {
		result SearchResult
		score  float64
		best   int
	}
	all := make(map[string]*fused, len(keyword)+len(vector))
	add := func(items []RankedChunk, channel string) {
		for _, item := range items {
			if item.Rank <= 0 || item.ChunkID == "" {
				continue
			}
			entry := all[item.ChunkID]
			if entry == nil {
				entry = &fused{result: item.Result, best: item.Rank}
				all[item.ChunkID] = entry
			}
			entry.score += 1.0 / float64(RRFConstant+item.Rank)
			if item.Rank < entry.best {
				entry.best = item.Rank
			}
			if !contains(entry.result.RetrievalChannels, channel) {
				entry.result.RetrievalChannels = append(entry.result.RetrievalChannels, channel)
			}
		}
	}
	add(keyword, "keyword")
	add(vector, "vector")
	items := make([]*fused, 0, len(all))
	for _, item := range all {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if items[i].best != items[j].best {
			return items[i].best < items[j].best
		}
		return items[i].result.ChunkID < items[j].result.ChunkID
	})
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]SearchResult, len(items))
	for i, item := range items {
		item.result.Rank = i + 1
		out[i] = item.result
	}
	return out
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// ValidateQuote checks a quote against the exact chunk text. It returns a
// Unicode code-point offset, not a byte offset, matching the public protocol.
func ValidateQuote(text, quote string, start, end *int) (int, int, error) {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return 0, 0, ErrQuoteNotFound
	}
	if start != nil && end != nil {
		runes := []rune(text)
		if *start < 0 || *end < *start || *end > len(runes) || string(runes[*start:*end]) != quote {
			return 0, 0, ErrQuoteNotFound
		}
		return *start, *end, nil
	}
	// Whitespace normalization is the only forgiving matching allowed for
	// evidence. Build a normalized rune stream with an offset for every output
	// rune, so matching is linear and the returned range still points into the
	// original Unicode code-point sequence.
	needle := []rune(NormalizeText(quote))
	normalized, offsets := normalizeWithOffsets([]rune(text))
	matches := [][2]int{}
	for i := 0; i+len(needle) <= len(normalized); i++ {
		matched := true
		for j := range needle {
			if normalized[i+j] != needle[j] {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, [2]int{offsets[i].start, offsets[i+len(needle)-1].end})
		}
	}
	if len(matches) == 0 {
		return 0, 0, ErrQuoteNotFound
	}
	if len(matches) > 1 {
		return 0, 0, ErrAmbiguousQuote
	}
	return matches[0][0], matches[0][1], nil
}

type normalizedOffset struct{ start, end int }

func normalizeWithOffsets(input []rune) ([]rune, []normalizedOffset) {
	result := make([]rune, 0, len(input))
	offsets := make([]normalizedOffset, 0, len(input))
	pendingSpace := false
	for i, value := range input {
		if unicode.IsSpace(value) {
			if len(result) > 0 {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			result = append(result, ' ')
			offsets = append(offsets, normalizedOffset{start: i - 1, end: i})
			pendingSpace = false
		}
		for _, normalized := range norm.NFKC.String(string(value)) {
			if normalized < utf8.RuneSelf {
				normalized = unicode.ToLower(normalized)
			}
			result = append(result, normalized)
			offsets = append(offsets, normalizedOffset{start: i, end: i + 1})
		}
	}
	return result, offsets
}
