package service

import "testing"

func TestBuildAgentSkillBundlesProducesRefsAndFullBundles(t *testing.T) {
	bundles, refs := BuildAgentSkillBundles([]AgentSkillData{
		{
			ID:          "skill-1",
			Name:        "deploy",
			Description: "Deploy safely",
			Content:     "main content",
			Files:       []AgentSkillFileData{{Path: "rules.md", Content: "rules"}},
		},
	})
	if len(bundles) != 1 || len(refs) != 1 {
		t.Fatalf("counts: bundles=%d refs=%d, want 1/1", len(bundles), len(refs))
	}
	if bundles[0].Content == "" || bundles[0].Files[0].Content == "" {
		t.Fatal("full bundle lost content")
	}
	ref := refs[0]
	if ref.ID != "skill-1" || ref.Hash == "" || ref.SizeBytes == 0 || ref.FileCount != 1 {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	if len(ref.Files) != 1 || ref.Files[0].Path != "rules.md" || ref.Files[0].SHA256 == "" || ref.Files[0].SizeBytes == 0 {
		t.Fatalf("unexpected file ref: %+v", ref.Files)
	}
}

func TestBuildAgentSkillBundlesAssignsBuiltinID(t *testing.T) {
	_, refs := BuildAgentSkillBundles([]AgentSkillData{{Name: "multica-working-on-issues", Content: "body"}})
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1", len(refs))
	}
	if refs[0].ID != "builtin:multica-working-on-issues" || refs[0].Source != "builtin" {
		t.Fatalf("builtin ref = %+v", refs[0])
	}
}
