package knowledge

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeURLKeepsQueryAndDropsFragment(t *testing.T) {
	got, err := NormalizeURL("HTTPS://Example.COM:443/report?q=1#section")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/report?q=1" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeURL("https://user:secret@example.com/report"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("userinfo URL error = %v", err)
	}
}

func TestVectorLiteralRejectsInvalidValuesAndNormalizes(t *testing.T) {
	literal, err := VectorLiteral([]float64{3, 4}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if literal != "[0.60000000000000009,0.8]" && literal != "[0.6,0.8]" {
		t.Fatalf("unexpected normalized vector %q", literal)
	}
	if _, err := VectorLiteral([]float64{0, 0}, 2); !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("zero vector error = %v", err)
	}
	if _, err := VectorLiteral([]float64{1}, 2); err == nil {
		t.Fatal("dimension mismatch accepted")
	}
}

func TestFuseRRFIsRankBasedAndStable(t *testing.T) {
	keyword := []RankedChunk{{ChunkID: "b", Rank: 1, Result: SearchResult{ChunkID: "b"}}, {ChunkID: "a", Rank: 2, Result: SearchResult{ChunkID: "a"}}}
	vector := []RankedChunk{{ChunkID: "a", Rank: 1, Result: SearchResult{ChunkID: "a"}}}
	got := FuseRRF(keyword, vector, 10)
	if len(got) != 2 || got[0].ChunkID != "a" || got[0].RetrievalChannels[0] != "keyword" || len(got[0].RetrievalChannels) != 2 {
		t.Fatalf("unexpected fusion: %+v", got)
	}
}

func TestValidateQuoteUsesCodePointOffsets(t *testing.T) {
	start, end := 0, 0
	gotStart, gotEnd, err := ValidateQuote("甲乙丙", "乙", &start, &end)
	if err == nil {
		t.Fatal("invalid explicit range accepted")
	}
	start, end = 1, 2
	gotStart, gotEnd, err = ValidateQuote("甲乙丙", "乙", &start, &end)
	if err != nil || gotStart != 1 || gotEnd != 2 {
		t.Fatalf("got %d:%d %v", gotStart, gotEnd, err)
	}
}

func TestChunkDocumentPreservesTableSheetBoundary(t *testing.T) {
	chunks := ChunkDocument(ParsedDocument{Blocks: []DocumentBlock{
		{BlockID: "a", Text: "a", Locator: map[string]any{"kind": "table", "sheet": "One"}},
		{BlockID: "b", Text: "b", Locator: map[string]any{"kind": "table", "sheet": "Two"}},
	}})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
}

func TestSearchTokensNormalizesAndDeduplicates(t *testing.T) {
	got := SearchTokens("ＡＩ AI 你好 你")
	want := []string{"ai", "你", "好"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

func TestKeywordIndexFieldsSeparateWeightedInputs(t *testing.T) {
	combined, title, path, body := keywordIndexFields("产品标题", Chunk{
		Text:        "正文内容",
		HeadingPath: []string{"章节一", "小节二"},
	})
	if title != "产品标题" || path != "章节一 小节二" || body != "正文内容" {
		t.Fatalf("weighted fields = %q, %q, %q", title, path, body)
	}
	if combined != "产品标题\n章节一 小节二\n正文内容" {
		t.Fatalf("combined keyword text = %q", combined)
	}
}

func TestChunkDocumentAddsBoundedOverlapWithoutCrossingSheets(t *testing.T) {
	first := strings.Repeat("甲乙丙丁", 260)
	second := "下一段内容"
	chunks := ChunkDocument(ParsedDocument{Blocks: []DocumentBlock{
		{BlockID: "one", Text: first, Locator: map[string]any{"kind": "table", "sheet": "One"}},
		{BlockID: "two", Text: second, Locator: map[string]any{"kind": "table", "sheet": "One"}},
		{BlockID: "three", Text: "另一张表", Locator: map[string]any{"kind": "table", "sheet": "Two"}},
	}})
	if len(chunks) < 2 {
		t.Fatalf("expected split chunks, got %+v", chunks)
	}
	for _, chunk := range chunks {
		if chunk.TokenEstimate > DefaultChunkMaxTokens {
			t.Fatalf("chunk exceeds max: %d", chunk.TokenEstimate)
		}
	}
	for index := 1; index < len(chunks); index++ {
		previousSheet := tableSheet(DocumentBlock{Locator: chunks[index-1].SourceLocator})
		currentSheet := tableSheet(DocumentBlock{Locator: chunks[index].SourceLocator})
		if previousSheet != "" && currentSheet != "" && previousSheet != currentSheet {
			if strings.Contains(chunks[index].Text, "甲乙丙丁") {
				t.Fatal("table overlap crossed a sheet boundary")
			}
		}
	}
}

func TestValidateGeneratedAnswerRequiresCitationForEveryAnswerParagraph(t *testing.T) {
	paragraphCounts := map[string]int{"chunk-1": 2}
	generated := generatedAnswer{
		Answer:    "第一段结论。\n\n第二段结论。",
		Citations: []AnswerCitation{{ID: "chunk-1", Paragraph: 1, AnswerParagraph: 1}},
	}
	if got := validateGeneratedAnswer(&generated, paragraphCounts); got != "missing_answer_paragraph_citations" {
		t.Fatalf("validation code = %q", got)
	}
	generated.Citations = append(generated.Citations, AnswerCitation{ID: "chunk-1", Paragraph: 2, AnswerParagraph: 2})
	if got := validateGeneratedAnswer(&generated, paragraphCounts); got != "" {
		t.Fatalf("valid answer rejected: %q", got)
	}
}

func TestValidateGeneratedAnswerBackfillsSingleParagraphMapping(t *testing.T) {
	generated := generatedAnswer{
		Answer:    "单段结论。",
		Citations: []AnswerCitation{{ID: "chunk-1", Paragraph: 1}},
	}
	if got := validateGeneratedAnswer(&generated, map[string]int{"chunk-1": 1}); got != "" {
		t.Fatalf("valid single paragraph answer rejected: %q", got)
	}
	if generated.Citations[0].AnswerParagraph != 1 {
		t.Fatalf("answer paragraph was not backfilled: %+v", generated.Citations[0])
	}
}
