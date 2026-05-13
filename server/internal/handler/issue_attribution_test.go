package handler

import (
	"strings"
	"testing"
)

func TestAnnotateDescriptionUpdate(t *testing.T) {
	tests := []struct {
		name      string
		oldDesc   string
		newDesc   string
		actorName string
		wantAttr  bool   // expect attribution divider
		wantOld   bool   // expect old content preserved
		wantNew   bool   // expect new content present
	}{
		{
			name:      "append content gets attribution",
			oldDesc:   "Original description",
			newDesc:   "Original description\n\nNew section added by agent",
			actorName: "TestBot",
			wantAttr:  true,
			wantOld:   true,
			wantNew:   true,
		},
		{
			name:      "full replacement has no attribution",
			oldDesc:   "Original description",
			newDesc:   "Completely different content",
			actorName: "TestBot",
			wantAttr:  false,
		},
		{
			name:      "empty old description has no attribution",
			oldDesc:   "",
			newDesc:   "Brand new description",
			actorName: "TestBot",
			wantAttr:  false,
		},
		{
			name:      "identical descriptions unchanged",
			oldDesc:   "Same content",
			newDesc:   "Same content",
			actorName: "TestBot",
			wantAttr:  false,
		},
		{
			name:      "whitespace-only append skipped",
			oldDesc:   "Content",
			newDesc:   "Content\n\n  \n",
			actorName: "TestBot",
			wantAttr:  false,
		},
		{
			name:      "trailing whitespace difference in old handled",
			oldDesc:   "Content  \n\n",
			newDesc:   "Content\n\nAppended text",
			actorName: "Agent007",
			wantAttr:  true,
			wantOld:   true,
			wantNew:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := annotateDescriptionUpdate(tt.oldDesc, tt.newDesc, tt.actorName)

			hasAttr := strings.Contains(result, "---\n*✏️ Updated by "+tt.actorName)
			if hasAttr != tt.wantAttr {
				t.Errorf("attribution presence = %v, want %v\nresult:\n%s", hasAttr, tt.wantAttr, result)
			}

			if tt.wantAttr {
				// Old content should be preserved at the start.
				oldTrimmed := strings.TrimRight(tt.oldDesc, " \t\n\r")
				if tt.wantOld && !strings.HasPrefix(result, oldTrimmed) {
					t.Errorf("old content not preserved at start\nresult:\n%s", result)
				}

				// Check attribution contains UTC timestamp pattern.
				if !strings.Contains(result, "(UTC)*") {
					t.Errorf("attribution missing UTC timestamp\nresult:\n%s", result)
				}
			}
		})
	}
}

func TestAnnotateDescriptionUpdatePreservesAppendedContent(t *testing.T) {
	old := "## Requirements\n\n- Feature A\n- Feature B"
	appended := "\n\n## Analysis\n\n- Finding 1\n- Finding 2"
	result := annotateDescriptionUpdate(old, old+appended, "ArchBot")

	if !strings.Contains(result, "## Analysis") {
		t.Error("appended content not found in result")
	}
	if !strings.Contains(result, "## Requirements") {
		t.Error("original content not found in result")
	}
	if !strings.Contains(result, "Updated by ArchBot") {
		t.Error("attribution with actor name not found")
	}

	// The attribution divider should appear between old and new content.
	attrIdx := strings.Index(result, "---\n*✏️ Updated by ArchBot")
	oldIdx := strings.Index(result, "## Requirements")
	newIdx := strings.Index(result, "## Analysis")
	if !(oldIdx < attrIdx && attrIdx < newIdx) {
		t.Errorf("attribution not between old and new content\nold=%d attr=%d new=%d\nresult:\n%s",
			oldIdx, attrIdx, newIdx, result)
	}
}
