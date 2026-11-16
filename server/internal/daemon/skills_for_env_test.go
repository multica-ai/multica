package daemon

import (
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestConvertSkillsForEnvPreservesBuiltinIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, id, wantBuiltin string
	}{
		{name: "builtin ref", source: "builtin", id: "builtin:multica-platform", wantBuiltin: "multica-platform"},
		{name: "builtin without id", source: "builtin", wantBuiltin: "multica-platform"},
		{name: "legacy builtin", wantBuiltin: "multica-platform"},
		{name: "workspace ref", source: "workspace", id: "e78e3700-7776-427a-a0bb-96a6f7b41af6"},
		{name: "legacy workspace", id: "e78e3700-7776-427a-a0bb-96a6f7b41af6"},
		{name: "explicit workspace without id", source: "workspace"},
		{name: "plugin", source: "plugin", id: "plugin:multica-platform"},
		{name: "unknown source without id", source: "external"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			skill := SkillData{
				ID: tc.id, Source: tc.source, Name: "multica-platform", Description: "Platform contracts.",
				Content: "---\nname: multica-platform\n---\n\nInstructions.",
				Files:   []SkillFileData{{Path: "references/sub-issues.md", Content: "Sub-issue contracts."}},
			}
			want := []execenv.SkillContextForEnv{{
				Name: skill.Name, BuiltinName: tc.wantBuiltin, Description: skill.Description, Content: skill.Content,
				Files: []execenv.SkillFileContextForEnv{{Path: skill.Files[0].Path, Content: skill.Files[0].Content}},
			}}
			if got := convertSkillsForEnv([]SkillData{skill}); !reflect.DeepEqual(got, want) {
				t.Fatalf("converted skills = %+v, want %+v", got, want)
			}
		})
	}
}
