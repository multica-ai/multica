package daemon

import (
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// activeSlashSkillMatches limits the existing slash-link grammar to real Markdown
// links outside quotations/code. A copied issue excerpt is never a selection.
func activeSlashSkillMatches(message string) []slashSkillMatch {
	matches := slashSkillMatches(message)
	if len(matches) == 0 {
		return matches
	}
	source := []byte(message)
	active := make(map[int]bool)
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if node.Kind() == ast.KindBlockquote {
			return ast.WalkSkipChildren, nil
		}
		link, ok := node.(*ast.Link)
		if !ok || !strings.HasPrefix(string(link.Destination), "slash://skill/") {
			return ast.WalkContinue, nil
		}
		_ = ast.Walk(link, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
			if value, ok := child.(*ast.Text); ok && entering {
				for _, match := range matches {
					if match.start <= value.Segment.Start && value.Segment.Stop <= match.end {
						active[match.start] = true
					}
				}
			}
			return ast.WalkContinue, nil
		})
		return ast.WalkSkipChildren, nil
	})
	selected := matches[:0]
	for _, match := range matches {
		if active[match.start] {
			selected = append(selected, match)
		}
	}
	return selected
}

// memberSkillText pairs an authenticated human-member input with the active
// matches found inside it. Only the server's own authorship data can put a
// text here; nothing derived from issue bodies, agent/system comments or
// legacy payloads without an author type qualifies.
type memberSkillText struct {
	text    string
	matches []slashSkillMatch
}

func chatSkillMatches(task Task) []slashSkillMatch {
	if task.ChatSkillMessage == nil {
		// Legacy servers did not distinguish the sender from the originator.
		// Keep their existing hint, but never issue a native command/injection.
		return slashSkillMatches(task.ChatMessage)
	}
	if *task.ChatSkillMessage == "" {
		// Known non-member chat keeps bound name hints without earning an
		// invocation. Quoted/code links are not current selections.
		return activeSlashSkillMatches(task.ChatMessage)
	}
	return activeSlashSkillMatches(*task.ChatSkillMessage)
}

func memberSkillTurn(task Task) bool {
	return task.ChatSessionID != "" && !task.ChatIntro && task.ChatSkillMessage != nil && *task.ChatSkillMessage != "" && task.WakeupID == "" && task.IssueID == "" && task.AutopilotRunID == "" && task.Agent != nil
}

// issueMemberSkillTurn reports whether this run is a comment-triggered issue
// task whose explicit skill selections may be honored at all. Wakeups and
// autopilots carry no member comment intent even when they name a trigger
// comment, and quick-create/assignment tasks have no comment to trust.
func issueMemberSkillTurn(task Task) bool {
	return task.IssueID != "" && task.ChatSessionID == "" && task.WakeupID == "" && task.AutopilotRunID == "" &&
		task.TriggerCommentID != "" && task.Agent != nil
}

// memberSkillTexts collects the authenticated member inputs this run may act
// on, in a stable order: a chat turn contributes its direct member message,
// an issue turn contributes its human-authored comments — coalesced comments
// oldest first, then the newest trigger comment. Everything else (agent or
// system authorship, empty author types from legacy payloads, quoted or
// code-span links via activeSlashSkillMatches) is filtered before it can
// reach a SkillInvocation.
func memberSkillTexts(task Task) []memberSkillText {
	if memberSkillTurn(task) {
		return []memberSkillText{{text: *task.ChatSkillMessage, matches: activeSlashSkillMatches(*task.ChatSkillMessage)}}
	}
	if !issueMemberSkillTurn(task) {
		return nil
	}
	var texts []memberSkillText
	for _, cc := range task.CoalescedComments {
		if cc.AuthorType == "member" && cc.Content != "" {
			texts = append(texts, memberSkillText{text: cc.Content, matches: activeSlashSkillMatches(cc.Content)})
		}
	}
	if task.TriggerAuthorType == "member" && task.TriggerCommentContent != "" {
		texts = append(texts, memberSkillText{text: task.TriggerCommentContent, matches: activeSlashSkillMatches(task.TriggerCommentContent)})
	}
	return texts
}

// memberSkillRefs deduplicates the refs of member texts by skill ID, keeping
// first-seen (chronological) order across the merged comments.
func memberSkillRefs(texts []memberSkillText) []SlashSkillRef {
	var matches []slashSkillMatch
	for _, text := range texts {
		matches = append(matches, text.matches...)
	}
	return uniqueSlashSkillRefs(matches)
}

// buildRuntimePrompt keeps ordinary inputs byte-for-byte unchanged. Only the
// server's authenticated member input can earn a structured invocation; a
// human originator on an agent/system/wakeup task is insufficient.
func buildRuntimePrompt(task Task, provider string, directories map[string]string, options ...PromptOption) (string, *agent.SkillInvocation) {
	texts := memberSkillTexts(task)
	var refs []SlashSkillRef
	if len(texts) > 0 {
		refs = memberSkillRefs(texts)
	} else if task.ChatSessionID != "" {
		// Legacy chat hint: refs stay display-only, never an invocation.
		refs = uniqueSlashSkillRefs(chatSkillMatches(task))
	}
	// The full slice expression copies the caller's variadic backing array:
	// later appends (the platform-prompt rebuild below, retries) must never
	// write through spare capacity the caller still owns.
	options = append(options[:len(options):len(options)], func(o *promptOpts) {
		o.skillDirectories = directories
		o.chatSkillRefs = refs
	})
	if len(texts) == 0 || len(refs) == 0 {
		return BuildPrompt(task, provider, options...), nil
	}
	assigned := make(map[string]SkillData, len(task.Agent.Skills))
	for _, skill := range task.Agent.Skills {
		assigned[skill.ID] = skill
	}
	invocation := &agent.SkillInvocation{}
	selected := make(map[string]string)
	for _, ref := range refs {
		skill, ok := assigned[ref.ID]
		dir := directories[ref.ID]
		if !ok || !filepath.IsAbs(dir) {
			continue
		}
		invocation.Skills = append(invocation.Skills, agent.ExplicitSkill{
			Name: filepath.Base(dir), Directory: dir, Body: execenv.SkillBody(skill.Content),
		})
		selected[ref.ID] = filepath.Base(dir)
	}
	if len(invocation.Skills) == 0 {
		return BuildPrompt(task, provider, options...), nil
	}
	if task.ChatSessionID != "" {
		return chatRuntimePrompt(task, texts[0], selected, invocation, provider, options...)
	}
	return issueRuntimePrompt(task, texts, selected, invocation, provider, options...)
}

// chatRuntimePrompt finishes a direct chat turn's invocation: the member
// message is a contiguous part of the claimed batch, and only selected links
// inside it are replaced by resolved names before the prompt is rebuilt.
func chatRuntimePrompt(task Task, source memberSkillText, selected map[string]string, invocation *agent.SkillInvocation, provider string, options ...PromptOption) (string, *agent.SkillInvocation) {
	skillMessage := source.text
	// Direct chat input is a contiguous suffix of the claimed batch; an older
	// or inconsistent server payload must not let this field replace user text.
	start := strings.LastIndex(task.ChatMessage, skillMessage)
	if start < 0 {
		return BuildPrompt(task, provider, options...), nil
	}
	// Replace only selected links in the actual member text. Unknown IDs and
	// quoted links remain visible, and cannot become a leading CLI command.
	prefix, suffix := task.ChatMessage[:start], task.ChatMessage[start+len(skillMessage):]
	user := replaceSelectedSkillLinks(skillMessage, source.matches, selected)
	task.ChatMessage = prefix + user + suffix
	invocation.UserMessage = task.ChatMessage
	// A resumed retry renders ChatResumeMessage instead of the full batch —
	// the server already dropped guidance delivered to PriorSessionID there —
	// and the trailing command block must not repeat it either. The member
	// batch is shorter in that view, so locate it by the boundary just
	// verified on the full batch and replace its selected links on its own
	// terms. The local task value re-points the field, so the caller's
	// payload stays intact for a later cold retry rebuilding from the task.
	if task.PriorSessionID != "" && task.ChatResumeMessage != nil {
		resume := *task.ChatResumeMessage
		if len(resume) >= len(prefix)+len(suffix) && strings.HasPrefix(resume, prefix) && strings.HasSuffix(resume, suffix) {
			body := resume[len(prefix) : len(resume)-len(suffix)]
			rewritten := prefix + replaceSelectedSkillLinks(body, activeSlashSkillMatches(body), selected) + suffix
			task.ChatResumeMessage = &rewritten
			invocation.UserMessage = rewritten
		}
	}
	invocation.Prompt = BuildPrompt(task, provider, options...)
	options = append(options, func(o *promptOpts) { o.omitChatMessage = true })
	invocation.PlatformPrompt = BuildPrompt(task, provider, options...)
	return invocation.FallbackPrompt(), invocation
}

// issueRuntimePrompt finishes a comment-triggered issue turn's invocation.
// Hit links are replaced in the human member comments embedded in the
// comment prompt, and UserMessage carries every member ask of the run —
// including coalesced member comments that carried no skill link — for
// Claude's trailing slash-command block. PlatformPrompt equals the complete
// comment prompt: unlike chat there is no separate user-message section to
// omit, and the issue context, thread metadata and --parent reply routing
// must survive intact.
func issueRuntimePrompt(task Task, texts []memberSkillText, selected map[string]string, invocation *agent.SkillInvocation, provider string, options ...PromptOption) (string, *agent.SkillInvocation) {
	// texts pairs every member comment of this run — coalesced comments
	// oldest first, then the trigger comment — with the matches
	// memberSkillTexts already found in it, so each comment is rewritten
	// exactly once.
	var user []string
	next := 0
	rewritten := func(content string) string {
		var matches []slashSkillMatch
		if next < len(texts) {
			matches = texts[next].matches
			next++
		}
		text := replaceSelectedSkillLinks(content, matches, selected)
		if message := strings.TrimSpace(text); message != "" {
			user = append(user, message)
		}
		return text
	}
	// The task value is a copy, but its slice fields share the caller's
	// backing array — the retry path rebuilds from the same task, and a
	// rewritten comment there would hide its links from the second pass.
	task.CoalescedComments = append([]CoalescedCommentData(nil), task.CoalescedComments...)
	for i, cc := range task.CoalescedComments {
		if cc.AuthorType == "member" && cc.Content != "" {
			task.CoalescedComments[i].Content = rewritten(cc.Content)
		}
	}
	if task.TriggerAuthorType == "member" && task.TriggerCommentContent != "" {
		content := rewritten(task.TriggerCommentContent)
		if strings.TrimSpace(content) == "" {
			// A pure skill-capsule comment strips to nothing. The comment
			// prompt treats an empty trigger body as "server could not fetch
			// it", which would silently drop the comment header, edit notes
			// and every coalesced comment — so keep the section alive with a
			// placeholder instead.
			content = ""
			options = append(options, func(o *promptOpts) { o.triggerCommentStripped = true })
		}
		task.TriggerCommentContent = content
	}
	invocation.UserMessage = strings.Join(user, "\n\n")
	invocation.Prompt = BuildPrompt(task, provider, options...)
	invocation.PlatformPrompt = invocation.Prompt
	return invocation.FallbackPrompt(), invocation
}

func replaceSelectedSkillLinks(message string, matches []slashSkillMatch, selected map[string]string) string {
	var user strings.Builder
	end := 0
	replaced, hasArguments := false, false
	for _, match := range matches {
		name, ok := selected[match.ref.ID]
		if !ok {
			continue
		}
		before := message[end:match.start]
		hasArguments = hasArguments || strings.TrimSpace(before) != ""
		user.WriteString(before)
		user.WriteString("/" + name)
		end = match.end
		replaced = true
	}
	// A message made entirely of selections invokes without arguments;
	// otherwise keep each resolved name beside the user's target text.
	if replaced && !hasArguments && strings.TrimSpace(message[end:]) == "" {
		return ""
	}
	user.WriteString(message[end:])
	return user.String()
}
