package knowledge

import (
	"fmt"
	"strings"
)

const (
	DefaultChunkTargetTokens  = 800
	DefaultChunkMaxTokens     = 1200
	DefaultChunkOverlapTokens = 100
)

// ChunkDocument groups nearby structural blocks without crossing table
// sheets. A long block is split by Unicode code points; its source block id is
// retained on every resulting chunk so citations remain traceable.
func ChunkDocument(parsed ParsedDocument) []Chunk {
	if len(parsed.Blocks) == 0 {
		return []Chunk{}
	}
	chunks := make([]Chunk, 0)
	var current []DocumentBlock
	currentTokens := 0
	var currentSheet string
	currentIsOverlap := false
	discard := func() {
		current = nil
		currentTokens = 0
		currentSheet = ""
		currentIsOverlap = false
	}
	flush := func(keepOverlap bool) {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, makeChunk(len(chunks), current))
		if keepOverlap {
			current = tailBlocks(current, DefaultChunkOverlapTokens)
			currentTokens = 0
			for _, block := range current {
				currentTokens += EstimateTokens(block.Text)
			}
			currentIsOverlap = len(current) > 0
			return
		}
		discard()
	}
	for _, block := range parsed.Blocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		sheet := tableSheet(block)
		// Keep model-derived blocks in separate chunks. This lets retrieval use
		// enhancement output while the graph extractor can exclude it until a
		// human confirms the block, without mixing confirmed source evidence and
		// unconfirmed model text in one citation unit.
		if len(current) > 0 && current[0].ModelDerived != block.ModelDerived {
			if currentIsOverlap {
				discard()
			} else {
				flush(false)
			}
		}
		if sheet != "" && currentSheet != "" && sheet != currentSheet {
			if currentIsOverlap {
				discard()
			} else {
				flush(false)
			}
		}
		if sheet != "" {
			currentSheet = sheet
		}
		// Leave room for the overlap that seeds the next chunk. The final
		// chunk is still bounded by DefaultChunkMaxTokens even when a part
		// follows an overlap block.
		parts := splitBlock(block, DefaultChunkMaxTokens-DefaultChunkOverlapTokens)
		for _, part := range parts {
			tokens := EstimateTokens(part.Text)
			if len(current) > 0 && (currentTokens+tokens > DefaultChunkTargetTokens || currentTokens+tokens > DefaultChunkMaxTokens) {
				flush(true)
				if sheet != "" {
					currentSheet = sheet
				}
			}
			current = append(current, part)
			currentTokens += tokens
			currentIsOverlap = false
			if currentTokens >= DefaultChunkMaxTokens {
				flush(true)
				if sheet != "" {
					currentSheet = sheet
				}
			}
		}
	}
	if !currentIsOverlap {
		flush(false)
	}
	return chunks
}

func tailBlocks(blocks []DocumentBlock, targetTokens int) []DocumentBlock {
	if targetTokens <= 0 || len(blocks) == 0 {
		return nil
	}
	remaining := targetTokens
	tail := make([]DocumentBlock, 0, len(blocks))
	for index := len(blocks) - 1; index >= 0 && remaining > 0; index-- {
		block := blocks[index]
		blockTokens := EstimateTokens(block.Text)
		if blockTokens <= remaining {
			tail = append(tail, block)
			remaining -= blockTokens
			continue
		}
		runes := []rune(block.Text)
		maxRunes := remaining * 4
		if maxRunes < 1 {
			break
		}
		if maxRunes > len(runes) {
			maxRunes = len(runes)
		}
		block.Text = string(runes[len(runes)-maxRunes:])
		block.Locator = cloneMap(block.Locator)
		block.Locator["overlap"] = true
		tail = append(tail, block)
		remaining = 0
	}
	for left, right := 0, len(tail)-1; left < right; left, right = left+1, right-1 {
		tail[left], tail[right] = tail[right], tail[left]
	}
	return tail
}

func splitBlock(block DocumentBlock, maxTokens int) []DocumentBlock {
	if EstimateTokens(block.Text) <= maxTokens {
		return []DocumentBlock{block}
	}
	runes := []rune(block.Text)
	maxRunes := maxTokens * 4
	if maxRunes < 1 {
		maxRunes = 1
	}
	parts := make([]DocumentBlock, 0, (len(runes)+maxRunes-1)/maxRunes)
	for start, part := 0, 0; start < len(runes); start, part = start+maxRunes, part+1 {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		copyBlock := block
		copyBlock.BlockID = fmt.Sprintf("%s-part-%d", block.BlockID, part+1)
		copyBlock.Text = string(runes[start:end])
		copyBlock.Locator = cloneMap(block.Locator)
		copyBlock.Locator["part"] = part + 1
		parts = append(parts, copyBlock)
	}
	return parts
}

func tableSheet(block DocumentBlock) string {
	if block.Locator == nil || block.Locator["kind"] != "table" {
		return ""
	}
	sheet, _ := block.Locator["sheet"].(string)
	return sheet
}

func makeChunk(ordinal int, blocks []DocumentBlock) Chunk {
	texts := make([]string, 0, len(blocks))
	refs := make([]string, 0, len(blocks))
	headingPath := make([]string, 0)
	seenHeadingPaths := make(map[string]struct{})
	var locator map[string]any
	for _, block := range blocks {
		path := ""
		if len(block.HeadingPath) > 0 {
			path = strings.Join(block.HeadingPath, " > ")
			if _, seen := seenHeadingPaths[path]; !seen {
				headingPath = append(headingPath, path)
				seenHeadingPaths[path] = struct{}{}
			}
		}
		if path != "" {
			texts = append(texts, path+"\n"+block.Text)
		} else {
			texts = append(texts, block.Text)
		}
		refs = append(refs, block.BlockID)
		if locator == nil {
			locator = cloneMap(block.Locator)
		}
	}
	if len(blocks) > 0 && blocks[0].ModelDerived {
		locator["model_derived"] = true
	}
	text := strings.TrimSpace(strings.Join(texts, "\n\n"))
	return Chunk{
		Ordinal:       ordinal,
		BlockRefs:     refs,
		Text:          text,
		HeadingPath:   headingPath,
		SourceLocator: locator,
		TokenEstimate: EstimateTokens(text),
		TextHash:      HashText(text),
	}
}

// keywordIndexFields returns the three weighted text fields required by the
// keyword index. The combined field is also kept for exact substring matches;
// the source chunk text itself remains unchanged for citations and previews.
func keywordIndexFields(title string, chunk Chunk) (combined, titleText, pathText, bodyText string) {
	titleText = NormalizeText(title)
	pathText = NormalizeText(strings.Join(chunk.HeadingPath, "\n"))
	bodyText = NormalizeText(chunk.Text)
	parts := make([]string, 0, 3)
	for _, value := range []string{titleText, pathText, bodyText} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n"), titleText, pathText, bodyText
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
