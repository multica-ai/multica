package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const oneShotTestWorkspace = "11111111-1111-4111-8111-111111111111"
const oneShotTestRuntime = "22222222-2222-4222-8222-222222222222"
const oneShotTestSkill = "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"
const oneShotTestSecondSkill = "33333333-3333-4333-8333-333333333333"

func TestOneShotContextRoundTrip(t *testing.T) {
	input := OneShotRunInput{
		RuntimeID:    oneShotTestRuntime,
		ModelID:      "deepseek-flash",
		Effort:       "high",
		Skills:       []string{oneShotTestSkill, oneShotTestSecondSkill, strings.ToLower(oneShotTestSkill)},
		SystemPrompt: []string{"Review the patch.\r\n", "Сохрани исходный текст.\n"},
		UserPrompt:   "<request>\r\n  Review `main` and $HOME literally.\r\n</request>\n",
	}
	ctx, err := NewOneShotContext(oneShotTestWorkspace, input)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Type != OneShotContextType || ctx.WorkspaceID != oneShotTestWorkspace {
		t.Fatalf("unexpected context identity: %+v", ctx)
	}
	if !reflect.DeepEqual(ctx.Skills, []string{strings.ToLower(oneShotTestSkill), oneShotTestSecondSkill}) {
		t.Fatalf("skills = %v, want canonical distinct skills in input order", ctx.Skills)
	}
	if ctx.ModelID != input.ModelID || ctx.Effort != input.Effort || ctx.UserPrompt != input.UserPrompt || !reflect.DeepEqual(ctx.SystemPrompt, input.SystemPrompt) {
		t.Fatalf("run configuration changed: %+v", ctx)
	}
	encoded, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var decoded OneShotContext
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, ctx) {
		t.Fatalf("context changed during JSON round-trip: %+v", decoded)
	}
	input.Skills[0] = "changed"
	input.SystemPrompt[0] = "changed"
	if ctx.Skills[0] != strings.ToLower(oneShotTestSkill) || ctx.SystemPrompt[0] != "Review the patch.\r\n" {
		t.Fatal("the job context retained mutable input slices")
	}
}

func TestOneShotContextAllowsRuntimeDefaults(t *testing.T) {
	ctx, err := NewOneShotContext(oneShotTestWorkspace, OneShotRunInput{
		RuntimeID: oneShotTestRuntime, UserPrompt: "Review the patch.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ModelID != "" || ctx.Effort != "" || len(ctx.Skills) != 0 || len(ctx.SystemPrompt) != 0 {
		t.Fatalf("unexpected optional configuration: %+v", ctx)
	}
}

func TestOneShotContextRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		workspace string
		input     OneShotRunInput
		field     string
	}{
		{"workspace", "invalid", OneShotRunInput{RuntimeID: oneShotTestRuntime, UserPrompt: "review"}, "workspace_id"},
		{"missing runtime", oneShotTestWorkspace, OneShotRunInput{UserPrompt: "review"}, "runtime_id"},
		{"runtime", oneShotTestWorkspace, OneShotRunInput{RuntimeID: "invalid", UserPrompt: "review"}, "runtime_id"},
		{"skill", oneShotTestWorkspace, OneShotRunInput{RuntimeID: oneShotTestRuntime, UserPrompt: "review", Skills: []string{"invalid"}}, "skills[0]"},
		{"empty prompt", oneShotTestWorkspace, OneShotRunInput{RuntimeID: oneShotTestRuntime}, "user_prompt"},
		{"blank prompt", oneShotTestWorkspace, OneShotRunInput{RuntimeID: oneShotTestRuntime, UserPrompt: " \r\n\t"}, "user_prompt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOneShotContext(tc.workspace, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error = %v, want invalid %s", err, tc.field)
			}
		})
	}
}
