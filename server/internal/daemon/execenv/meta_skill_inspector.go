package execenv

import (
	"github.com/multica-ai/multica/server/internal/cerebro/contextduplication"
)

// CEREBRO-PATCH(prompt-inspector): Expose the composed runtime brief for the read-only Cerebro inspector.
// MetaSkillInspection is the composed runtime brief split into sections for the
// Cost optimization prompt inspector (FIR-2765).
type MetaSkillInspection struct {
	Provider           string                            `json:"provider"`
	Sections           []contextduplication.Section      `json:"sections"`
	Duplication        contextduplication.Result         `json:"duplication"`
	FullMarkdown       string                            `json:"full_markdown"`
}

// InspectMetaSkill builds the same markdown the daemon injects into CLAUDE.md /
// AGENTS.md and scores cross-section duplication.
func InspectMetaSkill(provider string, ctx TaskContextForEnv) MetaSkillInspection {
	if provider == "" {
		provider = "claude"
	}
	md := buildMetaSkillContent(provider, ctx)
	sections := contextduplication.ParseSections(md)
	dup := contextduplication.Analyze(sections)
	return MetaSkillInspection{
		Provider:     provider,
		Sections:     sections,
		Duplication:  dup,
		FullMarkdown: md,
	}
}
