package agentoffice

// Context lint + drift lint (FIR-1775 Phase 3, requirement D and §9 move 2 of
// docs/agents/agent-office.md). Pure text analysis over an agent's composite
// context — no DB access here; the handler composes the inputs. Checks:
//
//   - dead_skill_ref        instructions reference a skill name that does not
//                           exist in the workspace
//   - unbound_skill_ref     instructions reference a workspace skill that is
//                           not bound to the agent
//   - duplicated_rule       the same substantive line appears in two layers
//                           (instructions vs a bound skill, or two skills)
//   - missing_context_owner / missing_approvers   empty governance fields
//   - stale_repo_link       a github.com link in the instructions that is not
//                           a known workspace/project repo
//
// Repo-file drift lint (CLAUDE.md / AGENTS.md content posted by the CLI):
//
//   - agent_behavior_in_repo_file   a line reads like agent-behavior (gates,
//                                   comms, mentions, persona) and belongs in
//                                   the versioned harness, not the repo file
//   - duplicated_from_harness       a line restates a rule already present in
//                                   an agent's instructions or a skill
//
// All checks are heuristics that produce review hints, never hard failures.

import (
	"fmt"
	"regexp"
	"strings"
)

// LintFinding is one drift/lint hint. Severity is "error" | "warning" | "info".
type LintFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// LintReport is the per-agent lint result.
type LintReport struct {
	AgentID        string        `json:"agent_id"`
	AgentName      string        `json:"agent_name"`
	ContextVersion string        `json:"context_version"`
	Findings       []LintFinding `json:"findings"`
}

// SkillDoc is the name + content of a skill, as the lint needs it.
type SkillDoc struct {
	ID      string
	Name    string
	Content string
}

// HarnessDoc is one searchable harness text (an agent's instructions or a
// workspace skill) for the repo-file drift lint.
type HarnessDoc struct {
	Kind string // "agent" | "skill"
	Name string
	Text string
}

// AgentLintInput carries everything LintAgentContext needs; the handler fills
// it from the DB so the check itself stays pure and unit-testable.
type AgentLintInput struct {
	Instructions        string
	BoundSkills         []SkillDoc
	WorkspaceSkillNames map[string]bool // lowercase name -> exists
	KnownRepoURLs       []string        // workspace + project github repos; empty = skip the repo check
	HasContextOwner     bool
	ApproverCount       int
}

// LintAgentContext runs every per-agent drift check and returns the findings
// in a stable order (governance, skill refs, duplicated rules, repo links).
func LintAgentContext(in AgentLintInput) []LintFinding {
	findings := []LintFinding{}
	if !in.HasContextOwner {
		findings = append(findings, LintFinding{
			Code:     "missing_context_owner",
			Severity: "warning",
			Message:  "agent has no context owner; change requests fall back to workspace admins only",
		})
	}
	if in.ApproverCount == 0 {
		findings = append(findings, LintFinding{
			Code:     "missing_approvers",
			Severity: "info",
			Message:  "agent has no context approvers; only the owner or a workspace admin can review proposals",
		})
	}
	bound := make(map[string]bool, len(in.BoundSkills))
	for _, s := range in.BoundSkills {
		bound[strings.ToLower(s.Name)] = true
	}
	findings = append(findings, lintSkillRefs(in.Instructions, in.WorkspaceSkillNames, bound)...)
	findings = append(findings, lintDuplicatedRules(in.Instructions, in.BoundSkills)...)
	findings = append(findings, lintRepoLinks(in.Instructions, in.KnownRepoURLs)...)
	return findings
}

// --- Skill references ---

// A skill reference is a backticked kebab-case name adjacent to the word
// "skill": `name`-skill, `name` skill, or skill `name`. Adjacency keeps the
// heuristic precise — a lone backticked token is usually code, not a skill.
var (
	skillRefAfter  = regexp.MustCompile("(?i)`([a-z0-9][a-z0-9-]*)`[ -]?skill")
	skillRefBefore = regexp.MustCompile("(?i)skill[a-z]*[:,]? +`([a-z0-9][a-z0-9-]*)`")
)

// lintSkillRefs flags referenced skill names that do not exist in the
// workspace (dead) or exist but are not bound to the agent (unbound).
func lintSkillRefs(instructions string, workspaceSkills, boundSkills map[string]bool) []LintFinding {
	findings := []LintFinding{}
	seen := map[string]bool{}
	for lineNo, line := range strings.Split(instructions, "\n") {
		for _, re := range []*regexp.Regexp{skillRefAfter, skillRefBefore} {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				name := strings.ToLower(m[1])
				if name == "skill" || seen[name] {
					continue
				}
				seen[name] = true
				switch {
				case !workspaceSkills[name]:
					findings = append(findings, LintFinding{
						Code:     "dead_skill_ref",
						Severity: "error",
						Message:  fmt.Sprintf("instructions reference skill %q which does not exist in the workspace", name),
						Line:     lineNo + 1,
						Detail:   truncateLine(line),
					})
				case !boundSkills[name]:
					findings = append(findings, LintFinding{
						Code:     "unbound_skill_ref",
						Severity: "warning",
						Message:  fmt.Sprintf("instructions reference skill %q which exists but is not bound to this agent", name),
						Line:     lineNo + 1,
						Detail:   truncateLine(line),
					})
				}
			}
		}
	}
	return findings
}

// --- Duplicated rules across layers ---

// lintDuplicatedRules flags substantive lines that appear in more than one
// layer of the composite (instructions vs a bound skill, or two bound skills).
// The daemon appends every bound skill to the instructions at claim time, so a
// duplicated line means the agent reads the same rule twice — and the copies
// will drift apart.
func lintDuplicatedRules(instructions string, skills []SkillDoc) []LintFinding {
	type source struct{ label, line string }
	byLine := map[string][]source{}
	index := func(label, text string) {
		seenHere := map[string]bool{}
		for _, raw := range strings.Split(text, "\n") {
			norm := normalizeRuleLine(raw)
			if norm == "" || seenHere[norm] {
				continue
			}
			seenHere[norm] = true
			byLine[norm] = append(byLine[norm], source{label: label, line: raw})
		}
	}
	index("instructions", instructions)
	for _, s := range skills {
		index("skill "+s.Name, s.Content)
	}

	findings := []LintFinding{}
	// Iterate instructions first, then skills, so output order is stable.
	reported := map[string]bool{}
	report := func(text string) {
		for _, raw := range strings.Split(text, "\n") {
			norm := normalizeRuleLine(raw)
			if norm == "" || reported[norm] || len(byLine[norm]) < 2 {
				continue
			}
			reported[norm] = true
			labels := make([]string, 0, len(byLine[norm]))
			for _, src := range byLine[norm] {
				labels = append(labels, src.label)
			}
			findings = append(findings, LintFinding{
				Code:     "duplicated_rule",
				Severity: "warning",
				Message:  "the same rule appears in " + strings.Join(labels, " and ") + "; keep one source of truth",
				Detail:   truncateLine(strings.TrimSpace(raw)),
			})
		}
	}
	report(instructions)
	for _, s := range skills {
		report(s.Content)
	}
	return findings
}

// normalizeRuleLine reduces a line to a comparable form: bullets and numbering
// stripped, whitespace collapsed. Headings, table rows, quotes, and short
// lines return "" (not substantive enough to call a duplicated rule).
var bulletPrefix = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)

func normalizeRuleLine(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "|") ||
		strings.HasPrefix(s, ">") || strings.HasPrefix(s, "```") {
		return ""
	}
	s = bulletPrefix.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) < 40 {
		return ""
	}
	return s
}

// --- Repo links ---

var githubRepoRE = regexp.MustCompile(`github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)`)

// lintRepoLinks flags github.com/{owner}/{repo} links in the instructions that
// are not among the workspace's known repos. When the workspace has no known
// repos at all, staleness cannot be determined and the check is skipped.
func lintRepoLinks(instructions string, knownRepoURLs []string) []LintFinding {
	known := map[string]bool{}
	for _, u := range knownRepoURLs {
		if slug := normalizeRepoSlug(u); slug != "" {
			known[slug] = true
		}
	}
	if len(known) == 0 {
		return nil
	}
	findings := []LintFinding{}
	seen := map[string]bool{}
	for lineNo, line := range strings.Split(instructions, "\n") {
		for _, m := range githubRepoRE.FindAllStringSubmatch(line, -1) {
			slug := normalizeRepoSlug(m[0])
			if slug == "" || seen[slug] || known[slug] {
				continue
			}
			seen[slug] = true
			findings = append(findings, LintFinding{
				Code:     "stale_repo_link",
				Severity: "warning",
				Message:  fmt.Sprintf("instructions link to repo %q which is not a known workspace/project repo", slug),
				Line:     lineNo + 1,
				Detail:   truncateLine(line),
			})
		}
	}
	return findings
}

// normalizeRepoSlug extracts a lowercase owner/repo slug from a github URL or
// slug, dropping a .git suffix and trailing punctuation.
func normalizeRepoSlug(u string) string {
	m := githubRepoRE.FindStringSubmatch(u)
	if m == nil {
		return ""
	}
	slug := strings.ToLower(m[1])
	slug = strings.TrimSuffix(slug, ".git")
	slug = strings.TrimRight(slug, ".,;:)")
	return slug
}

// --- Repo instruction file drift lint (CLAUDE.md / AGENTS.md) ---

// behaviorClasses are the language classes that mark a line as agent-behavior
// rather than code facts. Deliberately conservative: each phrase is specific
// enough that a code-facts line rarely contains it.
var behaviorClasses = []struct {
	class   string
	phrases []string
}{
	{"mention rules", []string{"mention://", "@mention", "never mention", "do not mention", "must be tagged"}},
	{"scheduling rules", []string{"schedule a wakeup", "schedule_wakeup", "set a wakeup", "wakeup before"}},
	{"communication style", []string{"always write in", "never write in", "write in danish", "write in english", "plain danish", "tone of voice", "start every response", "end every response"}},
	{"approval gates", []string{"ask for approval", "require approval", "ask before", "never post", "do not post", "always post", "without asking", "ask jesper", "ask the user first"}},
	{"persona", []string{"you are an agent", "your persona", "your role is", "your mission", "you are the "}},
	{"danish behavior text", []string{"du skal", "du må ikke", "spørg først", "aldrig ", "altid "}},
}

// LintRepoInstructionFile scans one repo CLAUDE.md/AGENTS.md's content for
// agent-behavior language (belongs in the versioned harness) and for lines
// duplicated from the harness (an agent's instructions or a workspace skill).
func LintRepoInstructionFile(filename, content string, harness []HarnessDoc) []LintFinding {
	harnessIndex := map[string]string{} // normalized line -> "agent Sabine" / "skill deploy"
	for _, doc := range harness {
		for _, raw := range strings.Split(doc.Text, "\n") {
			if norm := normalizeRuleLine(raw); norm != "" {
				if _, ok := harnessIndex[norm]; !ok {
					harnessIndex[norm] = doc.Kind + " " + doc.Name
				}
			}
		}
	}

	findings := []LintFinding{}
	inFence := false
	for lineNo, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			// Fenced code blocks are commands/config, not behavior prose.
			continue
		}
		if norm := normalizeRuleLine(line); norm != "" {
			if src, ok := harnessIndex[norm]; ok {
				findings = append(findings, LintFinding{
					Code:     "duplicated_from_harness",
					Severity: "warning",
					Message:  fmt.Sprintf("%s line restates a rule already in %s; keep it in the harness and reference it", filename, src),
					Line:     lineNo + 1,
					Detail:   truncateLine(strings.TrimSpace(line)),
				})
				continue
			}
		}
		lower := strings.ToLower(line)
		for _, bc := range behaviorClasses {
			matched := ""
			for _, p := range bc.phrases {
				if strings.Contains(lower, p) {
					matched = p
					break
				}
			}
			if matched == "" {
				continue
			}
			findings = append(findings, LintFinding{
				Code:     "agent_behavior_in_repo_file",
				Severity: "warning",
				Message:  fmt.Sprintf("%s line reads like agent behavior (%s, matched %q); it belongs in the versioned harness, not the repo file", filename, bc.class, matched),
				Line:     lineNo + 1,
				Detail:   truncateLine(strings.TrimSpace(line)),
			})
			break
		}
	}
	return findings
}

func truncateLine(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
