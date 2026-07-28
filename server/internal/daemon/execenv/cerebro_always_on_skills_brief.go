package execenv

import (
	"fmt"
	"strings"
)

// cerebroAlwaysOnSkillsBrief renders the "Always-on skills" section of the agent
// brief: the FULL text of every bound skill whose binding is flagged always-on.
//
// FIR-3805. A bound skill normally reaches the agent as one line (name +
// description) and is opened on demand. That is right for task recipes ("how to
// deploy") but wrong for rules that must hold on every single response — writing
// style, check-before-you-send gates. Those were never applied, because the
// agent never had a reason to open the skill before it started writing. Pasting
// the text into the brief removes the decision: the rules are simply present.
//
// The alternative — instructing the agent to open the skill first — was rejected
// on purpose: the text lands in the context either way, so it buys nothing but a
// round-trip, and the agent can drop the skill again mid-run.
//
// Kept in its own cerebro-prefixed sibling file so the upstream file
// (runtime_config.go) needs a single marked call-site instead of an inline patch.
func cerebroAlwaysOnSkillsBrief(skills []SkillContextForEnv) string { // CEREBRO-PATCH(skill-always-on): FIR-3805 new sibling file — always-on skill text for the brief
	var on []SkillContextForEnv
	for _, s := range skills {
		if s.AlwaysOn && strings.TrimSpace(s.Content) != "" {
			on = append(on, s)
		}
	}
	if len(on) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Always-on skills — active right now, not on demand\n\n")
	b.WriteString("The skills below are marked **always on** for you. Their full text is reproduced here because they govern EVERY response you produce — they are not reference material you decide whether to open. Treat them exactly as if they were written directly into your instructions above.\n\n")
	b.WriteString("They still appear in the skills list above; the copy here is the authoritative one for this run.\n\n")

	for _, s := range on {
		fmt.Fprintf(&b, "### Always-on skill: %s\n\n", s.Name)
		if desc := strings.TrimSpace(s.Description); desc != "" {
			fmt.Fprintf(&b, "%s\n\n", desc)
		}
		b.WriteString(strings.TrimSpace(s.Content))
		b.WriteString("\n\n")
	}

	// A long run compacts its context and dilutes the earliest instructions
	// first. Saying so keeps the agent from treating a faded rule as absent.
	b.WriteString("If this run grows long enough that the text above has been compacted away, the rules still stand — re-open the skill rather than assuming it no longer applies.\n\n")
	return b.String()
}

// cerebroAlwaysOnSkillSuffix marks an always-on entry in the one-line skills
// list, so the list and the reproduced text below it cannot look like two
// unrelated sets of skills. Empty for a normal load-on-demand binding.
func cerebroAlwaysOnSkillSuffix(s SkillContextForEnv) string {
	if !s.AlwaysOn || strings.TrimSpace(s.Content) == "" {
		return ""
	}
	return " **(always on — full text below)**"
}
