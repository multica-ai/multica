package execenv

import (
	"strings"
	"testing"
)

func TestGongfengRepositoryGuidance(t *testing.T) {
	for _, remote := range []string{
		"https://git.code.tencent.com/acme/widget.git",
		"git@git.code.tencent.com:acme/widget.git",
		"ssh://git@git.code.tencent.com/acme/widget.git",
	} {
		t.Run(remote, func(t *testing.T) {
			out := buildMetaSkillContent("claude", TaskContextForEnv{
				Repos: []RepoContextForEnv{{URL: remote, Ref: "release/b"}},
			})
			for _, want := range []string{"--target-branch <that-branch>", "gongfeng --project-id", "--base-url https://git.code.tencent.com", "user me --json", "does not switch the CLI account", "global MR `id`", "connection token is not passed"} {
				if !strings.Contains(out, want) {
					t.Errorf("guidance missing %q", want)
				}
			}
			if strings.Contains(out, "gh pr create") {
				t.Error("Gongfeng-only project received GitHub creation command")
			}
		})
	}
}

func TestGongfengGuidanceIsHostScoped(t *testing.T) {
	for _, remote := range []string{"https://github.com/acme/widget", "https://git.code.tencent.com.evil.test/acme/widget", "/tmp/git.code.tencent.com/widget"} {
		if isGongfengRepository(remote) {
			t.Errorf("incorrectly classified %q", remote)
		}
	}
	out := buildMetaSkillContent("claude", TaskContextForEnv{Repos: []RepoContextForEnv{
		{URL: "https://github.com/acme/widget", Ref: "release/a"},
		{URL: "https://git.code.tencent.com/acme/widget", Ref: "release/b"},
	}})
	if !strings.Contains(out, "gh pr create --base") || !strings.Contains(out, "gongfeng mr create") {
		t.Error("mixed provider project must describe both creation commands")
	}
}

func TestUnpinnedGongfengDoesNotInheritAnotherRepositoryTarget(t *testing.T) {
	out := buildMetaSkillContent("claude", TaskContextForEnv{Repos: []RepoContextForEnv{
		{URL: "https://github.com/acme/widget", Ref: "release/a"},
		{URL: "https://git.code.tencent.com/acme/widget"},
	}})
	if strings.Contains(out, "--target-branch <that-branch>") {
		t.Error("unpinned Gongfeng repository must resolve its own MR target")
	}
	if !strings.Contains(out, "--target-branch <target-branch>") {
		t.Error("Gongfeng MR creation should still name an explicit target")
	}
}
