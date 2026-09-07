package service

import (
	"regexp"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	"gopkg.in/yaml.v3"
)

// Built-in skills are the platform's standard "template" skills. These evals
// pin the template every skill must follow and — crucially — couple each
// skill's documented contract to the real backend behavior it describes, so a
// drift in the source-of-truth (e.g. the mention regex) breaks CI instead of
// silently turning the skill into a lie agents act on.
//
// The evals live in a _test.go file on purpose: anything *inside* a skill
// directory is walked into AgentSkillData.Files and shipped to agent machines
// (see loadBuiltinSkill). Tests must stay out of that payload.
//
// The same reasoning is what TestBuiltinSkillPayloadCarriesNoSourceReferences
// enforces for the payload's CONTENT (MUL-6986). A shipped skill is read on a
// machine that has no copy of this repository, so a file:line citation, a Go
// identifier, or a `go test` command in it is not merely useless — it sends the
// reader grepping for paths that do not exist. Evidence about how the server
// implements a contract belongs here, next to the assertion that pins it.

const (
	// maxSkillBodyLines is Anthropic's L2 budget for a SKILL.md body
	// (~5k tokens). Past this, content belongs in one-level-deep supporting
	// files, not the always-loaded body.
	maxSkillBodyLines = 500
	// maxRoutingBodyLines applies to a skill that HAS supporting references.
	// Its body is then a router — positioning, shared invariants, and a table
	// naming the reference to open — and every line it spends restating a
	// reference is a line every activation pays for. The whole point of
	// splitting into references is that the body stays cheap.
	maxRoutingBodyLines = 150
	// maxDescriptionChars is the frontmatter description cap — it is the only
	// thing an agent sees when deciding whether to load the skill, and every
	// runtime CLI pays for it in the always-loaded skill listing. A description
	// earns its characters two ways only: trigger wording that matches how the
	// task actually arrives, and reverse boundaries that prevent mis-routing.
	//
	// Naming several domains is not the "content inventory" this rules out. The
	// ban is on restating the BODY's section headings, which the agent reads for
	// free once it opens the skill. Nouns like "squad" or "autopilot" are how
	// the task arrives — they are trigger wording, and after MUL-6986 one
	// description has to do the recall work eight used to.
	maxDescriptionChars = 300
)

// TestBuiltinSkillsConformToTemplate enforces the standard-template invariants
// on every built-in skill, current and future. A new skill that violates the
// shape fails here without anyone having to remember the rules.
func TestBuiltinSkillsConformToTemplate(t *testing.T) {
	skills := allBuiltinSkillsForTest()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			// The multica- prefix is the platform namespace. Pointers name
			// built-ins by their bare name on the assumption that no
			// workspace skill shares one; nothing reserves the prefix
			// server-side yet, and that gap is accepted rather than handled
			// (see builtinSlug).
			if !strings.HasPrefix(skill.Name, "multica-") {
				t.Errorf("skill name %q must carry the multica- prefix", skill.Name)
			}

			fm, body, ok := splitFrontmatter(skill.Content)
			if !ok {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			if strings.TrimSpace(fm["name"]) == "" {
				t.Errorf("frontmatter is missing a non-empty name")
			}
			desc := strings.TrimSpace(fm["description"])
			if desc == "" {
				t.Errorf("frontmatter is missing a description (the only thing an agent sees when deciding to load the skill)")
			}
			if len(desc) > maxDescriptionChars {
				t.Errorf("description is %d chars, over the %d cap", len(desc), maxDescriptionChars)
			}
			budget := maxSkillBodyLines
			if len(skill.Files) > 0 {
				// A skill with references has already paid for the split; its
				// body must stay a router.
				budget = maxRoutingBodyLines
			}
			if n := strings.Count(body, "\n") + 1; n > budget {
				t.Errorf("SKILL.md body is %d lines, over the %d-line budget; move detail into one-level-deep supporting files", n, budget)
			}

			// Every built-in is a platform-contract skill: it triggers from
			// context rather than a slash command, and it is fenced to the CLI
			// it teaches.
			if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
				t.Errorf("user-invocable = %q, want false (a platform-contract skill triggers from context, not a slash command)", got)
			}
			// allowed-tools is the union across everything the skill now covers.
			// Merging eight skills merged their tool declarations too, so the
			// router declares Bash(git *) / Bash(gh *) — which only issues.md
			// needs — while an agent is reading, say, autopilots.md. The
			// alternative is dropping them and regressing the issue workflow
			// that legitimately runs `gh pr`. Recorded rather than silently
			// widened: allowed-tools is a pre-approval list, and today the
			// daemon's global permission mode means this grants no privilege
			// the agent did not already have. Revisit if per-reference
			// declarations ever exist.
			if got := strings.TrimSpace(fm["allowed-tools"]); !strings.Contains(got, "Bash(multica *)") {
				t.Errorf("allowed-tools = %q, want access to the Multica CLI", got)
			}

			for _, f := range skill.Files {
				if n := strings.Count(f.Content, "\n") + 1; n > maxSkillBodyLines {
					t.Errorf("supporting file %q is %d lines, over the %d-line budget; split it", f.Path, n, maxSkillBodyLines)
				}
			}

			// Evals must never ride along to agent machines as supporting files.
			for _, f := range skill.Files {
				lower := strings.ToLower(f.Path)
				if strings.Contains(lower, "eval") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.md") {
					t.Errorf("supporting file %q looks like an eval/test; evals belong in _test.go, not the shipped skill payload", f.Path)
				}
			}
		})
	}
}

// TestBuiltinSkillsFrontmatterIsStrictYAML is the regression guard for MUL-3100
// / GitHub #3851: a built-in SKILL.md whose frontmatter is not valid YAML 1.2
// (the canonical break is an unquoted `: ` inside the description) is silently
// dropped by strict runtimes like Codex, so the agent runs without that
// platform-contract skill.
//
// This check is deliberately separate from TestBuiltinSkillsConformToTemplate:
// that test reads the frontmatter through splitFrontmatter, a naive line parser
// that splits on the first ':' and never runs a YAML parse — so it passes even
// on the broken files. Only a real yaml.Unmarshal reproduces what Codex does,
// which is exactly what is needed to catch this class of bug before it ships.
func TestBuiltinSkillsFrontmatterIsStrictYAML(t *testing.T) {
	skills := allBuiltinSkillsForTest()
	if len(skills) == 0 {
		t.Fatal("no built-in skills loaded; embed or layout is broken")
	}

	for _, skill := range skills {
		t.Run(skill.Name, func(t *testing.T) {
			content := skill.Content
			if !strings.HasPrefix(content, "---\n") {
				t.Fatalf("SKILL.md must lead with a --- frontmatter block")
			}
			rest := content[len("---\n"):]
			end := strings.Index(rest, "\n---")
			if end < 0 {
				t.Fatalf("frontmatter has no closing --- delimiter")
			}

			var fm map[string]any
			if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
				t.Fatalf("frontmatter is not valid YAML — a strict runtime (e.g. Codex) "+
					"will drop this skill on load; quote values containing ': ': %v", err)
			}

			if name, ok := fm["name"].(string); !ok || strings.TrimSpace(name) == "" {
				t.Errorf("frontmatter name must parse as a non-empty string, got %#v", fm["name"])
			}
			if desc, ok := fm["description"].(string); !ok || strings.TrimSpace(desc) == "" {
				t.Errorf("frontmatter description must parse as a non-empty string, got %#v", fm["description"])
			}
		})
	}
}

// TestBuiltinSkillPayloadCarriesNoSourceReferences is the regression guard for
// MUL-6986: the shipped payload must not reference this repository's source.
//
// Every file in a skill directory is written into the workdir of every task
// that receives the skill — on the user's own machine, which has no checkout of
// this repo. A `file:line` citation there resolves to nothing, and a `go test`
// or `grep -n ... internal/handler/x.go` "verification command" actively sends
// the reading agent after paths that cannot exist. Before this test, roughly
// half the built-in payload by bytes was exactly that.
//
// Maintainer-facing evidence for a contract belongs in this file, beside the
// assertion that pins it, where it stays with the code it describes.
func TestBuiltinSkillPayloadCarriesNoSourceReferences(t *testing.T) {
	banned := []struct {
		what string
		re   *regexp.Regexp
	}{
		{"a path into this repository", regexp.MustCompile(`(?:server|packages|apps|e2e)/[A-Za-z0-9_-]+/`)},
		{"a source file name", regexp.MustCompile(`[A-Za-z0-9_./-]*\.(?:go|ts|tsx|sql)\b`)},
		{"a command that only runs in this repository", regexp.MustCompile(`\bgo (?:test|build|vet|run)\b`)},
		{"a migration ordinal", regexp.MustCompile(`\bmigrations?/[0-9]`)},
		{"a Go package-qualified identifier", regexp.MustCompile(`\b(?:util|service|handler|db|protocol|skillpkg|repocache|execenv|daemon)\.[A-Z][A-Za-z0-9]*`)},
		{"a Go test name", regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9]{3,}\b`)},
		{"a development branch name", regexp.MustCompile(`\b(?:feat|fix|refactor|chore)/[a-z0-9][a-z0-9-]*`)},
	}

	for _, skill := range allBuiltinSkillsForTest() {
		t.Run(skill.Name, func(t *testing.T) {
			files := map[string]string{"SKILL.md": skill.Content}
			for _, f := range skill.Files {
				files[f.Path] = f.Content
			}
			for path, content := range files {
				for _, b := range banned {
					if m := b.re.FindString(content); m != "" {
						t.Errorf("%s leaks %s (%q); the reader's machine has no copy of this repository — "+
							"state the user-observable behavior instead, and keep the evidence in builtin_skills_test.go", path, b.what, m)
					}
				}
			}
		})
	}
}

// TestPlatformSkillRoutingTableMatchesItsReferences keeps the router honest in
// both directions. A reference the table does not name is unreachable — the
// body is the only thing loaded on activation, so nothing else can point at it
// — and a table row naming a file that does not exist sends the agent to open
// a path that is not there.
func TestPlatformSkillRoutingTableMatchesItsReferences(t *testing.T) {
	skill, ok := findSkill(t, PlatformSkillName)
	if !ok {
		return
	}
	_, body, _ := splitFrontmatter(skill.Content)

	shipped := make(map[string]bool, len(skill.Files))
	for _, f := range skill.Files {
		shipped[f.Path] = true
		if !strings.HasPrefix(f.Path, "references/") {
			t.Errorf("supporting file %q is not under references/; the routing table only addresses that directory", f.Path)
		}
		if !strings.Contains(body, f.Path) {
			t.Errorf("SKILL.md routing table never names %q, so nothing can reach it", f.Path)
		}
	}

	for _, cited := range regexp.MustCompile("references/[a-z-]+\\.md").FindAllString(body, -1) {
		if !shipped[cited] {
			t.Errorf("SKILL.md points at %q, which the skill does not ship", cited)
		}
	}

	if len(skill.Files) == 0 {
		t.Fatal("platform skill ships no references; the routing body has nothing to route to")
	}
}

// TestLegacyRedirectsFollowTheDaemonsBrief pins the compatibility contract for
// the merge (MUL-6986).
//
// The runtime brief is assembled by the DAEMON, so deploying a backend does not
// rewrite an installed daemon's copy of it. A daemon released before the merge
// still tells its agent to "read the `multica-working-on-issues` skill" — a
// name this server no longer ships — and backend upgrades do not force a daemon
// upgrade, so that window is open-ended. Such a daemon gets a redirect stub;
// a current one gets nothing extra, which is what lets the stub retire itself.
//
// The stub must stay a signpost: if it ever grows contracts of its own they
// will rot against references/issues.md, silently, on exactly the installs that
// cannot be updated from here.
func TestLegacyRedirectsFollowTheDaemonsBrief(t *testing.T) {
	const legacy = "multica-working-on-issues"
	svc := &TaskService{}

	current := svc.BuiltinSkills("", false)
	if named(current, legacy) {
		t.Errorf("a current daemon still receives %q; the stub only exists for briefs that name it", legacy)
	}
	if !named(current, PlatformSkillName) {
		t.Fatalf("a current daemon does not receive %q", PlatformSkillName)
	}

	old := svc.BuiltinSkills("", true)
	if !named(old, legacy) {
		t.Errorf("a pre-merge daemon does not receive %q, so its brief points at a skill that is not installed", legacy)
	}
	if !named(old, PlatformSkillName) {
		t.Errorf("a pre-merge daemon lost %q; the redirect adds to the set, it does not replace it", PlatformSkillName)
	}

	// Mika's scoping is orthogonal to the redirect: both dimensions compose.
	if !named(svc.BuiltinSkills(MikaSystemKey, true), "multica-onboarding") {
		t.Errorf("Mika on a pre-merge daemon lost multica-onboarding")
	}
	if named(svc.BuiltinSkills("", true), "multica-onboarding") {
		t.Errorf("the redirect path leaked multica-onboarding to an ordinary agent")
	}

	var stub AgentSkillData
	for _, s := range old {
		if s.Name == legacy {
			stub = s
		}
	}
	if len(stub.Files) != 0 {
		t.Errorf("redirect stub ships %d supporting files; it should carry nothing but the new location", len(stub.Files))
	}
	_, body, ok := splitFrontmatter(stub.Content)
	if !ok {
		t.Fatal("redirect stub has no frontmatter")
	}
	if !strings.Contains(body, PlatformSkillName) || !strings.Contains(body, "references/issues.md") {
		t.Errorf("redirect stub does not name where the contracts went:\n%s", body)
	}
	if n := strings.Count(body, "\n") + 1; n > 40 {
		t.Errorf("redirect stub is %d lines; a signpost that grows contracts will rot against the real reference", n)
	}

	// Bundle resolution must still serve it: the stub reaches the daemon as a
	// ref like any other built-in, and the resolve path is name-blind.
	if !named(svc.AllBuiltinSkills(), legacy) {
		t.Errorf("the unscoped set is missing %q; bundle resolution would 404 for a pre-merge daemon", legacy)
	}
}

// TestPlatformSkillDescriptionNamesEveryDomain is the recall guard for the
// nine-into-one merge (MUL-6986).
//
// Before the merge, each domain advertised its own description in the
// always-loaded listing, so an agent looking for "how does squad routing work"
// matched on the word "squad". Collapsing to one skill removes seven of those
// eight surfaces, and the remaining description is the only thing an agent sees
// before deciding to open the skill. If a domain's trigger word is not in it,
// that domain became strictly harder to find than it was before — which is the
// one regression this merge is not allowed to cause.
//
// The map must also stay exhaustive: adding a reference without adding its
// trigger word ships contracts that nothing advertises.
func TestPlatformSkillDescriptionNamesEveryDomain(t *testing.T) {
	// reference file -> the word an agent would use when the task arrives.
	// These are task nouns, not section headings: a task arrives as "set up an
	// autopilot", never as "Core model".
	triggerWords := map[string]string{
		"references/issues.md":       "issue",
		"references/mentions.md":     "mention",
		"references/agents.md":       "agent",
		"references/squads.md":       "squad",
		"references/autopilots.md":   "autopilot",
		"references/projects.md":     "project",
		"references/runtimes.md":     "runtime",
		"references/skill-import.md": "skill import",
	}

	skill, ok := findSkill(t, PlatformSkillName)
	if !ok {
		return
	}
	fm, _, _ := splitFrontmatter(skill.Content)
	desc := strings.ToLower(fm["description"])

	for _, f := range skill.Files {
		word, mapped := triggerWords[f.Path]
		if !mapped {
			t.Errorf("reference %q has no trigger word; add one here and to the description, "+
				"or an agent has no way to learn this skill covers it", f.Path)
			continue
		}
		if !strings.Contains(desc, word) {
			t.Errorf("description never says %q, so %s is unreachable from the skill listing", word, f.Path)
		}
	}

	shipped := make(map[string]bool, len(skill.Files))
	for _, f := range skill.Files {
		shipped[f.Path] = true
	}
	for path := range triggerWords {
		if !shipped[path] {
			t.Errorf("trigger word mapped for %q, which the skill no longer ships", path)
		}
	}
}

// TestPlatformSkillCoversPlatformContracts re-homes the per-domain contract
// anchors that each merged skill used to assert on its own body. They are
// anchors, not prose review: if a contract leaves the payload, the brief
// pointers and the product decisions behind them go stale silently.
func TestPlatformSkillCoversPlatformContracts(t *testing.T) {
	skill, ok := findSkill(t, PlatformSkillName)
	if !ok {
		return
	}
	files := make(map[string]string, len(skill.Files)+1)
	files["SKILL.md"] = skill.Content
	for _, f := range skill.Files {
		files[f.Path] = f.Content
	}

	cases := []struct {
		file    string
		want    []string
		notWant []string
	}{
		{
			file: "SKILL.md",
			want: []string{
				// Must-fix from review: "load on demand" is not "load exactly
				// one". A task that creates a squad, assigns it an issue and
				// writes a mention needs three references, and wording that
				// implies one would have the agent act on contracts it never
				// read — the recall loss the merge is not allowed to cause.
				"open the reference(s) your task actually needs",
				"crosses domains needs each domain it touches",
				"Do not read them all",
				// The router's whole job: name every domain and its file.
				"references/issues.md",
				"references/mentions.md",
				"references/agents.md",
				"references/squads.md",
				"references/autopilots.md",
				"references/projects.md",
				"references/runtimes.md",
				"references/skill-import.md",
				// Invariants deduplicated out of the eight merged bodies. Each
				// was repeated in most of them; the router is now their only
				// home, so losing one here loses it everywhere.
				"A name is not an id",
				"`--output json` writes to stdout",
				"`--no-start` when you are only recording",
				"Status is a category, not a literal",
				"Comment reads stay bounded",
				"--roots-only --summary --compact",
				"--thread <thread-id> --tail 30",
			},
			notWant: []string{
				// The singular forms this replaced.
				"open the ONE reference",
				"there is never a reason to read all eight",
			},
		},
		{
			file: "references/issues.md",
			want: []string{
				"multica issue pull-requests <issue-id> --output json",
				"Default for code-changing issue work",
				"open or update a PR before posting the final Multica issue comment",
				"This is a default, not",
				"Use a routable issue key in the PR title, body, or branch",
				"include the PR URL when a PR exists",
				"Closes MUL-123",
				"--status backlog",
				// The only sanctioned pr_url reference is the negative
				// compatibility warning about pre-existing data — not a write
				// recommendation (MUL-5442 owner ruling: no curated key
				// vocabulary).
				"`pr_url` metadata (which can be",
				// MUL-5442: the brief's Sub-issue Creation section is a
				// one-line map pointing here. These anchors are the demoted
				// playbook — if they leave, the brief pointer dangles.
				"todo starts work now, backlog parks it",
				"`--stage <N>`",
				"when a whole stage finishes",
				"multica issue status <child-id> todo",
				// MUL-5442: the brief's Issue Metadata section defers the full
				// write discipline here. Every relocated ban is anchored
				// individually — both defining categories AND each example —
				// so no single item or category boundary can be dropped while
				// the brief still points here.
				"Never store secrets, tokens, or API keys",
				"Not metadata: logs or summaries",
				"bookkeeping such as timestamps",
				"attempt counts, or agent IDs",
				"other single-run details",
				"files touched and investigation notes",
				"belong in the result comment",
				"the platform curates no vocabulary",
				// #7768: nothing about concurrent runs is pushed into the
				// prompt any more (MUL-6984), so the skill has to carry the
				// pull path itself. All three anchors are load-bearing — the
				// command is the remedy, the cap disclosure stops a truncated
				// answer from reading as an empty one, and the advisory line
				// is a negative safety boundary: an agent that reads these as
				// a lock will skip the coordination they exist to prompt.
				"multica issue runs <issue-id> --siblings --output json",
				"capped at 20",
				"Nothing here reserves an issue or serialises anything",
			},
			notWant: []string{
				// A curated key list is the "recommended fields" concept the
				// owner ruled out on MUL-5442.
				"High-signal keys",
				"reuse these names so queries stay consistent",
				"scratchpad for run state",
				"(`pr_url`, `waiting_on`",
				// Per-turn workflow the runtime brief owns; duplicating it here
				// is how the two drift apart.
				"Start from the trigger, not from memory",
				"multica issue comment list <issue-id> --thread <trigger-comment-id>",
				"multica issue comment add <issue-id> --parent <trigger-comment-id>",
			},
		},
		{
			file: "references/mentions.md",
			want: []string{
				"(member|agent|squad|issue|all)/([0-9a-fA-F-]+|all)",
				"multica workspace member list --output json",
				"enqueues a run for that agent",
				"enqueues NOTHING",
				"[@all](mention://all/all)",
				"invocation_not_allowed",
				"target_unavailable",
				"runtime_offline",
				"coalesced",
				"deferred",
				// The autopilot-creator fallback is authorization only; an
				// agent reading it as attribution would mis-report who ran.
				"It is authorization only",
			},
		},
		{
			file: "references/agents.md",
			want: []string{
				"not a parameter manual",
				"`description` is a catalog summary",
				"`instructions` is the runtime behavior contract",
				"`conversation_starters`",
				"`avatar_url` → a random `emoji:<glyph>`",
				"multica agent create --name <name> --runtime-id <runtime-id>",
				"`model` is a first-class persisted column",
				"custom_env",
				"Never put credentials or other secrets in `custom_args`",
				"--custom-env-stdin",
				"--custom-env-file",
				"multica agent skills add <agent-id> --skill-ids <skill-id> --output json",
				"multica agent skills list <agent-id> --output json",
				"multica agent get <agent-id> --output json",
				"255",
			},
			notWant: []string{
				"--from-template",
				"/api/agent-templates",
				"template_slug",
				"curated template",
				// De-coaching: this states contracts, it does not teach a
				// generic how-to methodology.
				"Define the job first",
				"Run a low-risk task",
				"Decision flow",
			},
		},
		{
			file: "references/squads.md",
			want: []string{
				"A squad is not an agent",
				"squad's `leader_id` agent",
				"squad members are not automatically fanned out",
				"multica squad member set-role",
				"mention://squad/<squad-id>",
				"recording squad activity",
				// The debugging entry point must stay a bounded two-step read
				// (MUL-5442): a roots-only scan alone never returns reply
				// bodies, where mention triggers and failure reasons live.
				"--roots-only --summary",
				"--thread <thread-id> --tail 30",
				"scan the roots first, then open the threads",
			},
			notWant: []string{
				// MUL-5696: no unbounded comment pull. Both shapes contradict
				// the brief's "two bounded reads, never one bulk pull".
				"multica issue comment list <issue-id> --output json",
				"--recent 10",
			},
		},
		{
			file: "references/autopilots.md",
			want: []string{
				"An autopilot is not an agent",
				"create_issue",
				"run_only",
				"multica autopilot trigger-add <autopilot-id> --kind schedule",
				"multica autopilot trigger <autopilot-id> --output json",
				"Do not run `trigger`",
				"webhook tokens",
				"{{date}}",
				"squad's leader agent",
				// A schedule with no --timezone silently runs in UTC, which is
				// the one wrong assumption here that recurs daily.
				"runs in **UTC**",
			},
		},
		{
			file: "references/runtimes.md",
			want: []string{
				"the daemon polls and claims the task",
				"multica runtime list --output json",
				"multica repo checkout <url>",
				"MULTICA_DAEMON_PORT",
				"resource_ref.ref",
				"github_repo",
				"local_directory",
				"Runtime and repo commands affect active agent execution",
				// An agent reads this to know whether its checkout can be
				// committed to. Codex on Linux and Windows gets task-local Git
				// metadata; every other runtime gets a linked worktree.
				"Linux and Windows Codex",
				"task-local Git metadata",
			},
		},
		{
			file: "references/projects.md",
			want: []string{
				"Projects are durable context containers",
				".multica/project/resources.json",
				"multica project resource list <project-id> --output json",
				"multica project resource add <project-id> --type github_repo --url <github-url> --output json",
				"multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json",
				"multica project resource add <project-id> --type local_directory",
				"Project resources are durable and affect future tasks",
				"github_repo.resource_ref.url",
				"resource_ref.ref",
			},
		},
		{
			file: "references/skill-import.md",
			want: []string{
				"multica skill import --url <url> --output json",
				"/api/skills/import",
				"clawhub.ai",
				"skills.sh",
				"github.com",
				"config.origin",
				"--on-conflict fail",
				"--on-conflict overwrite",
				"--on-conflict rename",
				"--on-conflict skip",
				"conflict",
				"skipped",
				"409",
				"existing_skill",
				"legacy",
				"multica skill list --output json",
				"npx skills add",
				"multica agent skills add <agent-id> --skill-ids <skill-id> --output json",
				"multica agent skills list <agent-id> --output json",
				"replace-all",
				"`set` is the replacement path",
			},
			notWant: []string{
				"multica agent skills set <agent-id> --skill-ids <skill-id>",
				"merge the new skill id with the existing ids",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			content, ok := files[tc.file]
			if !ok {
				t.Fatalf("platform skill does not ship %q", tc.file)
			}
			for _, want := range tc.want {
				if !containsUnwrapped(content, want) {
					t.Errorf("%s missing %q", tc.file, want)
				}
			}
			for _, forbidden := range tc.notWant {
				if containsUnwrapped(content, forbidden) {
					t.Errorf("%s carries banned content %q", tc.file, forbidden)
				}
			}
		})
	}
}

// TestPlatformSkillTeachesTheParserContract is the eval that gives the mentions
// reference its value: it proves the skill teaches exactly what
// util.ParseMentions enforces. The skill's "Incorrect" examples must parse to
// nothing (the @gpt-boy class of bug: a name where a UUID belongs fails
// silently), and its "Correct" example must parse. If the mention regex drifts,
// this breaks and the skill's claims must be re-checked.
func TestPlatformSkillTeachesTheParserContract(t *testing.T) {
	const uuid = "7f3a1b2c-0000-4000-8000-000000000abc"

	cases := []struct {
		name    string
		content string
		want    []util.Mention
	}{
		{
			// Skill: "Writing [@Alice](mention://member/Alice) does NOTHING."
			// 'l'/'i' are not hex, so the id fails to parse — link is dead.
			name:    "name where a uuid belongs is silently dead",
			content: "[@Alice](mention://member/Alice) please review",
			want:    nil,
		},
		{
			// Skill: a bare @name is plain text, nobody is notified.
			name:    "bare @name is plain text",
			content: "@alice please review",
			want:    nil,
		},
		{
			// Skill Step 2: type and id source matched → fires.
			name:    "real uuid with matching type fires",
			content: "[@Alice](mention://member/" + uuid + ") please review",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
		{
			// Skill: @all uses the literal `all`, never a UUID.
			name:    "all uses the literal all",
			content: "[@all](mention://all/all) heads up",
			want:    []util.Mention{{Type: "all", ID: "all"}},
		},
		{
			// Skill: "Using the wrong type for a real UUID still parses — it
			// just resolves to the wrong entity." Which is exactly why the
			// skill stresses matching type to id source.
			name:    "wrong type still parses (points at wrong entity)",
			content: "[@Bot](mention://member/" + uuid + ")",
			want:    []util.Mention{{Type: "member", ID: uuid}},
		},
		{
			// Skill: `project` is deliberately outside the type group, so a
			// project reference can never start a run.
			name:    "project is not a parseable mention type",
			content: "[Roadmap](mention://project/" + uuid + ")",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := util.ParseMentions(tc.content)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseMentions(%q) = %+v, want %+v", tc.content, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("mention[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestOnboardingSkillIsScopedToMika pins the agent-scoping rule (MUL-6986).
//
// The onboarding walkthrough is one agent's procedure, not a platform contract.
// Shipping it to every agent put its description in every agent's always-loaded
// skill listing and its body in every task workdir, to be usable by one.
func TestOnboardingSkillIsScopedToMika(t *testing.T) {
	const onboarding = "multica-onboarding"

	ordinary := loadBuiltinSkills("")
	if named(ordinary, onboarding) {
		t.Errorf("an ordinary agent still receives %q", onboarding)
	}
	if !named(ordinary, PlatformSkillName) {
		t.Errorf("an ordinary agent does not receive %q, which every agent needs", PlatformSkillName)
	}

	mika := loadBuiltinSkills(MikaSystemKey)
	if !named(mika, onboarding) {
		t.Errorf("Mika does not receive %q, which only Mika can use", onboarding)
	}
	if !named(mika, PlatformSkillName) {
		t.Errorf("Mika lost %q; scoping must add to the universal set, not replace it", PlatformSkillName)
	}

	// The resolve path deliberately serves any built-in by id — the claim is
	// the gate, and re-deriving the scope there would cost an agent read to
	// re-answer a question the claim already answered.
	if !named(allBuiltinSkillsForTest(), onboarding) {
		t.Errorf("the unscoped set is missing %q; bundle resolution would 404 for Mika's daemon", onboarding)
	}
}

// containsUnwrapped matches an anchor against prose that Markdown has hard
// wrapped. These anchors pin a claim, not a line layout — matching raw bytes
// made every reflow of a paragraph look like a deleted contract, which trains
// authors to fix the test instead of the text.
func containsUnwrapped(content, want string) bool {
	return strings.Contains(collapseSpace(content), collapseSpace(want))
}

var whitespaceRun = regexp.MustCompile(`\s+`)

func collapseSpace(s string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(s, " "))
}

func named(skills []AgentSkillData, name string) bool {
	for _, s := range skills {
		if s.Name == name {
			return true
		}
	}
	return false
}

// allBuiltinSkillsForTest includes the legacy redirect stubs. They are shipped
// payload like any other built-in, so the template, frontmatter and
// source-leak suites must hold for them as well.
func allBuiltinSkillsForTest() []AgentSkillData {
	return append(loadBuiltinSkillDirs(func(string) bool { return true }), legacyRedirectSkills()...)
}

func findSkill(t *testing.T, name string) (AgentSkillData, bool) {
	t.Helper()
	for _, s := range allBuiltinSkillsForTest() {
		if s.Name == name {
			return s, true
		}
	}
	t.Errorf("built-in skill %q not found", name)
	return AgentSkillData{}, false
}

// splitFrontmatter returns the top-level scalar keys of a leading YAML
// frontmatter block, the body after it, and whether a block was found. It only
// understands flat `key: value` lines — enough for the template's frontmatter.
func splitFrontmatter(content string) (map[string]string, string, bool) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, content, false
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, content, false
	}
	block := rest[:end]
	body := rest[end:]
	if nl := strings.Index(body, "\n"); nl >= 0 {
		body = body[nl+1:] // drop the closing --- line
	}

	fm := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // nested value; the template uses only flat scalars
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		fm[strings.TrimSpace(key)] = value
	}
	return fm, body, true
}
