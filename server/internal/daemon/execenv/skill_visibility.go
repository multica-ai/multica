package execenv

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// modelVisibleSkills returns the skills a model may invoke, with Name
// normalized to the on-disk slug.
//
// The normalization is not cosmetic. Every caller renders these entries into a
// listing the model reads to pick a skill, and the only name it can actually
// invoke is the directory slug writeSkillFiles lays down — sanitizeSkillName of
// the same field. A workspace skill's Name is its human display name ("PR
// review"), so listing it verbatim hands the model an identifier that does not
// resolve (MUL-5529). Normalizing here rather than at each call site keeps the
// four listings (runtime brief + the three issue_context renderers) from
// drifting apart.
//
// Slugs come from resolveSkillSlugs over the *unfiltered* batch, because
// writeSkillFiles lays down every skill — including the ones hidden here — and
// each one consumes a slug. Filtering first would shift the suffixes and make
// the listing disagree with the directories.
//
// Known gap: resolveSkillSlugs cannot see the filesystem, so a skill that
// collides with a *user-installed* directory is still written to
// `<slug>-multica` while the listing shows the bare slug. Closing that needs
// the allocated slug threaded back from Prepare, which these renderers
// deliberately cannot reach — they are pure so the brief stays byte-identical
// across runs. Tracked in MUL-5550.
func modelVisibleSkills(skills []SkillContextForEnv) []SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	slugs := resolveSkillSlugs(skills)
	visible := make([]SkillContextForEnv, 0, len(skills))
	for i, skill := range skills {
		if skillModelInvocationVisible(skill) {
			skill.Name = slugs[i]
			visible = append(visible, skill)
		}
	}
	return visible
}

// resolveSkillSlugs assigns each skill in a batch its on-disk directory slug,
// deduplicating within the batch so two skills never claim the same directory.
//
// sanitizeSkillName alone is not injective: "A B" and "A-B" both reduce to
// "a-b". writeSkillFiles resolves that at write time via
// allocateCollisionFreeSkillDir, so the second skill lands in `a-b-multica` —
// but a listing built from sanitizeSkillName alone would name both `a-b`,
// leaving the second skill with no invocable name and silently pointing the
// model at the first. Deriving both sides from this function keeps them in
// step. It is deterministic in the batch (index order only), so the brief stays
// byte-identical across runs for identical input.
func resolveSkillSlugs(skills []SkillContextForEnv) []string {
	slugs := make([]string, len(skills))
	taken := make(map[string]struct{}, len(skills))
	for i, skill := range skills {
		base := sanitizeSkillName(skill.Name)
		slug := base
		for attempt := 1; ; attempt++ {
			if _, clash := taken[slug]; !clash {
				break
			}
			slug = skillSlugCandidate(base, attempt)
		}
		taken[slug] = struct{}{}
		slugs[i] = slug
	}
	return slugs
}

func skillModelInvocationVisible(skill SkillContextForEnv) bool {
	return !skillDisablesModelInvocation(skill.Content)
}

func skillDisablesModelInvocation(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(fmBody), &data); err != nil {
		return false
	}
	value, ok := data["disable-model-invocation"]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}
