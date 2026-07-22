package loops

import "testing"

func TestExtractSlashSkills(t *testing.T) {
	refs := ExtractSlashSkills("Use [/plan](slash://skill/skill-1) then [/review](slash://skill/skill-2) and [/plan](slash://skill/skill-1).")
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want two unique skills", refs)
	}
	if refs[0].ID != "skill-1" || refs[0].Label != "plan" || refs[1].ID != "skill-2" {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestExtractSlashSkillsIgnoresUnknownProtocols(t *testing.T) {
	if refs := ExtractSlashSkills("[/plan](slash://action/skill-1)"); len(refs) != 0 {
		t.Fatalf("refs = %+v, want none", refs)
	}
}
