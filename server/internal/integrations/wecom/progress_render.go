package wecom

// progress_render.go — turning what the agent is doing into something a
// colleague on WeCom can read, and keeping everything else in the run out of
// the chat.
//
// The daemon already broadcasts the transcript: task:message fires for every
// tool call, every tool result, every chunk of prose, batched about twice a
// second. That is the only mid-run signal fine-grained enough to answer "what
// is it doing right now" — task:progress fires exactly twice per run and both
// lines are for an operator, not a user. So this file consumes the transcript
// and emits a sentence.
//
// TWO RULES decide what ends up in the chat, and they are independent.
//
// WHO — progressLevel, below. Outside the principal's own one-to-one chat a
// step produces nothing at all. WeCom has no redaction of its own and no
// unsend, a group bubble is read by the whole room, and a colleague's chat is
// somebody else's private conversation. That gate is absolute and comes first.
//
// WHAT — a step may name the ARGUMENT that identifies the work: the path, the
// command with its flags, the search term, the URL, the brief handed to a
// subagent, the parameters of an MCP call, the text of an error. Those are
// what make a step worth reading rather than a category, and the person
// reading them owns the run.
//
// A step may NEVER carry a CONTENT BLOCK: a file's body, either half of an
// edit, a patch's diff, a command's output, a page's text, a subagent's
// report. Two reasons, and the second one holds even where the first does
// not. They are 8KB apiece against a 20KB bubble, so one of them costs the
// whole window and says less than the word it replaced. And nobody reads a
// file in a chat bubble — the web transcript is where the body goes.
//
// Everything arriving on tool_result is a content block by definition, which
// is why that whole message type is refused before it costs a database read.
// On the input side the bodies are named keys, and progressBodyKeys is the
// list.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// progressMaxLines is how many steps the bubble shows. The window scrolls:
	// the newest work is what tells the user it is still moving, and a bubble
	// that grows without bound eventually costs more to read than it says.
	progressMaxLines = 8

	// progressMinInterval is the floor between two refresh frames of one
	// bubble. Tool calls arrive several a second; every frame is a write on
	// the bot's single socket and counts against the platform's own limits, so
	// steps inside the window are folded into the next frame rather than each
	// getting one. The closing frame never passes through here.
	progressMinInterval = 1500 * time.Millisecond

	// progressFragmentRunes caps the argument a line may carry, so a
	// pathological one cannot turn a step into a paragraph.
	//
	// 160 is chosen from the three things it has to hold at once. A phone in
	// WeCom fits roughly 20 full-width or 40 half-width characters to a line,
	// so 160 is at most four wrapped lines — long enough to read, short enough
	// that a step still looks like a step. It clears the real arguments with
	// room to spare: an absolute path runs 40-90 characters and a command with
	// its flags 40-120, and cutting either at 40, as this used to, cut it in
	// the middle. And the window's worst case stays inside the protocol's
	// budget — eight lines of 160 CJK runes is under 4KB against the 20480 the
	// server takes.
	progressFragmentRunes = 160

	// progressMaxArgs bounds how many parameters of one call get rendered
	// before the length cap would cut them off anyway.
	progressMaxArgs = 8

	// progressThinkingRunes is how much of the agent's reasoning the bubble
	// keeps. Thinking has no natural end — it arrives as 500ms increments for
	// as long as the run lasts — so what is kept is the tail, and this is how
	// long that tail is.
	//
	// 1200 is about a screen and a half of phone text: enough to follow a
	// train of thought, short enough that the step list above it is still on
	// the first screen. It also has to fit beside everything else in one
	// frame: 1200 CJK runes is under 4KB, the eight-line step window is under
	// 4KB more, and the server takes 20480.
	progressThinkingRunes = 1200
)

// progressLevel is how much of a run one bubble may show. It is decided once,
// when the message is ingested and the audience is known, and carried on the
// stream handle for the bubble's whole life.
//
// Two tiers and no middle one. A run's steps are the principal's own working
// notes; anyone else in the conversation is a bystander to them, and a tier
// that showed bystanders "reading a file" without saying which would be honest
// but would still put a scrolling activity log into somebody else's chat. So
// the choice is the whole list or none of it.
type progressLevel uint8

const (
	// progressLevelNone shows the bubble and the answer, and nothing in
	// between. It is the zero value on purpose: a handle nobody classified
	// shows nothing.
	progressLevelNone progressLevel = iota

	// progressLevelDetail shows the run as it happens, arguments included.
	progressLevelDetail
)

// progressKind is what a step is, independent of language. The event arrives
// on a bus that knows nothing about installations, so a step is classified
// first and worded later, once the bubble's own locale is known.
type progressKind uint8

const (
	// progressRaw is a line produced by the run itself (a task:progress
	// summary). It is printed as given, only trimmed to one line.
	progressRaw progressKind = iota
	progressRead
	progressEdit
	progressCommand
	progressSearch
	progressWeb
	progressSubtask
	progressPlan
	progressService
	progressSkill
	progressTool
	progressError

	// progressThinking is the odd one out: not a thing the agent did but what
	// it was reasoning while doing it. It arrives as increments and lands in
	// the feed's rolling tail rather than as a line in the step list.
	progressThinking
)

// progressStep is one thing the agent did, already stripped of everything no
// audience may see. Every field is filled by a helper below — never straight
// from the payload, so the sanitising and the length cap cannot be skipped.
type progressStep struct {
	kind progressKind

	// arg is the argument that identifies the work: the path, the command,
	// the search term, the URL, the brief, the error. For an MCP call it is
	// the server, and arg2 the operation.
	arg  string
	arg2 string

	// args is a call's remaining parameters, rendered as a list of key=value.
	// Only tools this adapter has no words for use it — an MCP call and an
	// unknown tool — because for everything else the one argument above says
	// more than a parameter dump would.
	args string
}

// progressKindByTool maps the tool names the providers actually emit onto the
// kinds this adapter has words for. Keys are lowercased tool names, and one
// workspace can run any provider, so they all live in one table.
//
// Where the names come from, because that is what makes this list checkable
// rather than a guess: Claude Code and CodeBuddy pass the tool's own name
// through (claude.go); Codex hardcodes exec_command and patch_apply
// (codex.go); Cursor strips the suffix off "<name>ToolCall" (cursor.go); and
// the ACP family — Qoder, Kimi, Kiro, Hermes — normalises the ACP title to
// snake_case and passes an unrecognised one through as snake_case (hermes.go,
// kimi.go, kiro.go), which is how the Gemini-CLI vocabulary that qwen-code
// inherits arrives. progress_tools_test.go holds the same inventory.
//
// A name missing from here still produces a line — see stepFromToolUse's
// default branch — so a new tool degrades to "正在使用 X" rather than to
// silence. That degradation is safe and reads badly, which is the whole reason
// to keep this table current: a run on a provider whose vocabulary is missing
// is a wall of English identifiers in Chinese copy.
var progressKindByTool = map[string]progressKind{
	// reading
	"read":            progressRead,
	"read_file":       progressRead,
	"readfile":        progressRead,
	"notebookread":    progressRead,
	"view":            progressRead,
	"open_file":       progressRead,
	"read_many_files": progressRead,
	// An image is read too, and the path is the useful half of the line.
	"vision_analyze": progressRead,

	// changing files
	"write":              progressEdit,
	"write_file":         progressEdit,
	"edit":               progressEdit,
	"edit_file":          progressEdit,
	"multiedit":          progressEdit,
	"multi_edit":         progressEdit,
	"notebookedit":       progressEdit,
	"str_replace_editor": progressEdit,
	"str_replace":        progressEdit,
	"apply_patch":        progressEdit,
	"apply_diff":         progressEdit,
	"patch_apply":        progressEdit,
	"patch":              progressEdit,
	"create_file":        progressEdit,
	"replace":            progressEdit,
	"code":               progressEdit,
	"delete":             progressEdit,
	"delete_file":        progressEdit,
	"remove_file":        progressEdit,

	// running things
	"bash":              progressCommand,
	"bashoutput":        progressCommand,
	"killshell":         progressCommand,
	"shell":             progressCommand,
	"exec":              progressCommand,
	"exec_command":      progressCommand,
	"run_terminal_cmd":  progressCommand,
	"run_shell_command": progressCommand,
	"terminal":          progressCommand,
	"execute_code":      progressCommand,
	"run_code":          progressCommand,
	// A slash command is a named command the agent runs, and the name is
	// exactly what the line should carry.
	"slashcommand":  progressCommand,
	"slash_command": progressCommand,

	// looking through the code
	"grep":                progressSearch,
	"glob":                progressSearch,
	"search":              progressSearch,
	"file_search":         progressSearch,
	"codebase_search":     progressSearch,
	"search_files":        progressSearch,
	"search_file_content": progressSearch,
	"list_dir":            progressSearch,
	"list_directory":      progressSearch,
	"list_files":          progressSearch,
	"ls":                  progressSearch,

	// looking things up outside
	"webfetch":          progressWeb,
	"web_fetch":         progressWeb,
	"websearch":         progressWeb,
	"web_search":        progressWeb,
	"web_extract":       progressWeb,
	"google_web_search": progressWeb,
	"fetch":             progressWeb,

	// handing work off
	"task":           progressSubtask,
	"agent":          progressSubtask,
	"dispatch_agent": progressSubtask,
	"delegate_task":  progressSubtask,

	// running a packaged procedure — its own kind because a skill is not
	// reading, editing, running or searching, and the skill's name is what
	// the line is worth reading for
	"skill": progressSkill,

	// planning
	"todowrite":     progressPlan,
	"todoread":      progressPlan,
	"todo_write":    progressPlan,
	"updatetodos":   progressPlan,
	"update_todos":  progressPlan,
	"update_plan":   progressPlan,
	"exitplanmode":  progressPlan,
	"enterplanmode": progressPlan,
	// The ACP families expose deliberation as a tool call of its own. It is
	// the agent working something out, which is the plan line's subject.
	"thinking": progressPlan,
}

// stepFromTaskMessage classifies one task:message, or reports that it is not
// progress at all.
//
// Tool calls, errors and thinking qualify. What does not: tool_result, whose
// output is the content block this file exists to keep out of the chat, and
// text, which is the answer being written and gets its own closing frame.
// Rejecting those two here is what keeps this subscriber cheap — together they
// are most of the event's volume and neither reaches the database lookup
// behind this function.
func stepFromTaskMessage(payload any) (progressStep, bool) {
	msg := taskMessageFields(payload)
	switch msg.msgType {
	case "tool_use":
		return stepFromToolUse(msg.tool, msg.input), true
	case "error":
		return progressStep{kind: progressError, arg: safeFragment(msg.content)}, true
	case "thinking":
		return progressStep{kind: progressThinking, arg: safeThinking(msg.content)}, true
	}
	return progressStep{}, false
}

// taskMessage is the slice of a task:message payload this file reads. Output
// is deliberately absent: it is the content block, every time, whatever the
// tool.
type taskMessage struct {
	msgType string
	tool    string
	content string
	input   map[string]any
}

// taskMessageFields reads that slice off a payload, typed in-process or in its
// map form after a serialization round trip.
func taskMessageFields(payload any) taskMessage {
	switch p := payload.(type) {
	case protocol.TaskMessagePayload:
		return taskMessage{msgType: p.Type, tool: p.Tool, content: p.Content, input: p.Input}
	case *protocol.TaskMessagePayload:
		if p == nil {
			return taskMessage{}
		}
		return taskMessage{msgType: p.Type, tool: p.Tool, content: p.Content, input: p.Input}
	case map[string]any:
		var m taskMessage
		m.msgType, _ = p["type"].(string)
		m.tool, _ = p["tool"].(string)
		m.content, _ = p["content"].(string)
		m.input, _ = p["input"].(map[string]any)
		return m
	}
	return taskMessage{}
}

// stepFromToolUse decides what a tool call is doing and picks the argument
// worth naming. Every branch names keys it wants rather than sweeping the
// input, which is what keeps a file's body out of a step about the file: the
// body arrives under a key too, and a step that took whatever it found would
// take that as readily as the path.
func stepFromToolUse(tool string, input map[string]any) progressStep {
	if server, name, ok := mcpToolParts(tool); ok {
		return progressStep{kind: progressService, arg: server, arg2: name, args: argsFragment(input)}
	}
	switch progressKindByTool[strings.ToLower(strings.TrimSpace(tool))] {
	case progressRead:
		return progressStep{kind: progressRead, arg: pathFragment(input)}
	case progressEdit:
		return progressStep{kind: progressEdit, arg: pathFragment(input)}
	case progressCommand:
		return progressStep{kind: progressCommand, arg: commandFragment(input)}
	case progressSearch:
		return progressStep{kind: progressSearch, arg: searchFragment(input)}
	case progressWeb:
		return progressStep{kind: progressWeb, arg: webFragment(input)}
	case progressSubtask:
		return progressStep{kind: progressSubtask, arg: subtaskFragment(input)}
	case progressPlan:
		return progressStep{kind: progressPlan, arg: planFragment(input)}
	case progressSkill:
		return progressStep{kind: progressSkill, arg: skillFragment(input)}
	}
	// An unknown tool still gets a line: a step the user never sees happen is
	// indistinguishable from a run that has stalled. With no idea which of its
	// parameters matters, it gets the list.
	return progressStep{kind: progressTool, arg: safeFragment(tool), args: argsFragment(input)}
}

// line words a step in one locale, for one audience. The only values
// interpolated are the step's own fragments, which the vetting helpers have
// already cleaned.
//
// The tier check is HERE, at the single point where a step becomes words,
// rather than in each branch below or in each caller. That is what makes it
// hold for a tool type nobody has written yet: a new kind gets a case in this
// switch, and inherits the gate whether its author thought about audiences or
// not. recordStep has a cheaper check of its own, but this is the one that is
// exhaustive.
func (s progressStep) line(c copyPack, level progressLevel) string {
	if level != progressLevelDetail {
		return ""
	}
	p := c.Progress
	switch s.kind {
	case progressRaw:
		return s.arg
	case progressThinking:
		// Already cleaned by safeThinking; the feed routes it to the tail
		// rather than to the step list. It passes through here so the tier
		// gate above covers it like everything else.
		return s.arg
	case progressRead:
		if s.arg == "" {
			return p.ReadPlain
		}
		return fmt.Sprintf(p.Read, s.arg)
	case progressEdit:
		if s.arg == "" {
			return p.EditPlain
		}
		return fmt.Sprintf(p.Edit, s.arg)
	case progressCommand:
		if s.arg == "" {
			return p.Command
		}
		return fmt.Sprintf(p.CommandNamed, s.arg)
	case progressSearch:
		if s.arg == "" {
			return p.Search
		}
		return fmt.Sprintf(p.SearchNamed, s.arg)
	case progressWeb:
		if s.arg == "" {
			return p.Web
		}
		return fmt.Sprintf(p.WebNamed, s.arg)
	case progressSubtask:
		if s.arg == "" {
			return p.Subtask
		}
		return fmt.Sprintf(p.SubtaskNamed, s.arg)
	case progressPlan:
		if s.arg == "" {
			return p.Plan
		}
		return fmt.Sprintf(p.PlanNamed, s.arg)
	case progressService:
		if s.args == "" {
			return fmt.Sprintf(p.Service, s.arg, s.arg2)
		}
		return fmt.Sprintf(p.ServiceArgs, s.arg, s.arg2, s.args)
	case progressSkill:
		if s.arg == "" {
			return p.SkillPlain
		}
		return fmt.Sprintf(p.Skill, s.arg)
	case progressError:
		if s.arg == "" {
			return p.Failed
		}
		return fmt.Sprintf(p.FailedNamed, s.arg)
	case progressTool:
		switch {
		case s.arg == "":
			return p.Fallback
		case s.args == "":
			return fmt.Sprintf(p.Tool, s.arg)
		}
		return fmt.Sprintf(p.ToolArgs, s.arg, s.args)
	}
	return ""
}

// progressBodyKeys names the input keys that carry a CONTENT BLOCK rather than
// an argument — a file being written, either half of an edit, a notebook cell,
// a patch. They are the same thing tool_result carries, arriving on the way in
// instead of the way out, and they are excluded for the same two reasons: one
// of them is routinely the whole 20KB budget, and a file body is not something
// anyone reads in a chat bubble.
//
// Keys are matched lowercased. Only argsFragment consults this list — the
// per-kind helpers below name the keys they want, so a body key never comes up
// for them.
var progressBodyKeys = map[string]bool{
	"content":        true,
	"contents":       true,
	"text":           true,
	"body":           true,
	"data":           true,
	"source":         true,
	"new_source":     true,
	"newsource":      true,
	"new_string":     true,
	"newstring":      true,
	"old_string":     true,
	"oldstring":      true,
	"new_str":        true,
	"old_str":        true,
	"file_text":      true,
	"streamcontent":  true,
	"stream_content": true,
	"diff":           true,
	"patch":          true,
	"changes":        true,
	"edits":          true,
	"replacement":    true,
	"output":         true,
	"result":         true,
}

// firstString returns the first of the named keys that holds a non-empty
// string, cleaned. Keys are tried in order, so the most specific one wins.
func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		v, _ := input[key].(string)
		if frag := safeFragment(v); frag != "" {
			return frag
		}
	}
	return ""
}

// pathFragment returns the file a tool call names, path and all.
//
// The directory used to be dropped along with the contents, on the grounds
// that a path can be a customer's name or an unreleased project's. It still
// can — and the person now reading it is the one who asked for the work, in
// their own chat, which is the whole point of the tier this runs under. The
// base name alone was ambiguous exactly when it mattered: four handler.go in
// one repo look identical in a bubble.
func pathFragment(input map[string]any) string {
	return firstString(input,
		"file_path", "path", "notebook_path", "target_file", "filename", "file",
		"directory", "dir", "absolute_path")
}

// commandFragment returns the command as the agent wrote it, flags and all.
//
// This used to return the program name only, and drop it entirely when a
// wrapper shell got there first — so `bash -lc 'go test ./...'`, which is how
// half of them arrive, read as "正在执行命令" and said nothing. The flags are
// the part that distinguishes a run from a rerun.
func commandFragment(input map[string]any) string {
	return firstString(input, "command", "cmd", "script", "command_line", "commandline")
}

// searchFragment returns what a search was for. The term is the step: "正在
// 检索代码" is true of every search a run makes and separates none of them.
func searchFragment(input map[string]any) string {
	return firstString(input,
		"pattern", "query", "regex", "search_term", "searchterm", "keyword", "q",
		"glob", "path", "file_path", "directory")
}

// webFragment returns the URL fetched or the question asked.
func webFragment(input map[string]any) string {
	return firstString(input, "url", "query", "q", "search_query", "prompt")
}

// subtaskFragment returns the brief handed to a subagent — the one place a
// step can say what a run is delegating rather than that it delegated.
func subtaskFragment(input map[string]any) string {
	return firstString(input, "prompt", "description", "task", "instructions", "subagent_type")
}

// planFragment returns the plan being written. A todo list arrives as an array
// of objects rather than a string, which firstString declines; the plain line
// covers it.
func planFragment(input map[string]any) string {
	return firstString(input, "plan", "description", "summary", "title")
}

// skillFragment returns the skill being run. The tool is always called Skill;
// which skill it loaded is the whole content of the step.
func skillFragment(input map[string]any) string {
	return firstString(input, "skill", "name", "command", "id")
}

// argsFragment renders a call's parameters as key=value, for the two kinds of
// tool this adapter has no words for: an MCP call and a name it has never
// seen. Both are exactly the cases where the parameters are the only thing
// that says what is happening.
//
// A content block is excluded twice over, by name and by shape, because the
// name alone cannot do it. progressBodyKeys lists the 24 keys tools were
// observed to put a body under, and a list only knows what somebody already
// wrote down: the next provider spells it `code`, or `file_contents`, or
// renames `new_string` to `new_text`, and a denylist hands the file straight
// to the chat. That is not a hypothetical — it leaks whatever is on the first
// line of the file, which for an .env is the secret, into a WeCom message
// nobody can unsend.
//
// So the value decides too, and it fails closed: anything spanning more than
// one line is a body, and so is anything longer than an argument could
// usefully be on a phone. What survives is what argsFragment is for — a path,
// a command, a query, an id, a flag.
//
// Being honest about the limit: a SHORT, single-line value under an unknown
// key is indistinguishable from an argument, and this shows it. The claim this
// enforces is therefore "no body reaches the bubble", not "nothing the tool
// was given reaches the bubble" — the latter is what the two-tier gate is for,
// and it is why this whole line is confined to the principal's own 1:1 and
// never appears in a group.
//
// Also excluded: a nested object or array, because it is either a body or a
// structure that would not fit on a line either way, and an empty value, which
// says nothing. Keys are sorted so the same call reads the same way twice —
// the input is a map, Go's iteration order is not stable, and a step that
// reshuffles itself between frames looks like a different step.
func argsFragment(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		if progressBodyKeys[strings.ToLower(strings.TrimSpace(k))] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		var val string
		switch v := input[k].(type) {
		case string:
			val = v
		case bool:
			val = strconv.FormatBool(v)
		case float64:
			val = strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			val = strconv.Itoa(v)
		case json.Number:
			val = v.String()
		default:
			continue
		}
		if strings.TrimSpace(val) == "" {
			continue
		}
		if looksLikeBody(val) {
			continue
		}
		parts = append(parts, k+"="+val)
		// The join is capped anyway; stopping early keeps a tool with fifty
		// parameters from building a string only to throw it away.
		if len(parts) >= progressMaxArgs {
			break
		}
	}
	return safeFragment(strings.Join(parts, ", "))
}

// argMaxRunes is the longest a value can be and still be identifying WORK
// rather than being the work. A path, a shell command with its flags, a search
// query and a URL all fit; a file, a captured stdout and a page of markdown do
// not. Set above the longest argument seen in practice rather than tight, so
// the newline test below carries the weight and this only catches a body that
// happens to be minified onto one line.
const argMaxRunes = 128

// looksLikeBody reports whether a value is content rather than an argument,
// from its shape alone — which is the only signal available when the key is
// one nobody has seen before.
//
// More than one line is the load-bearing test: a file, a diff, a captured
// output and a markdown page all span lines, and an argument does not. Length
// is the backstop for the one-line blob. Both are deliberately crude; the
// question they answer is not "what is this" but "could a person read this on
// one line of a chat bubble and learn what the agent is doing".
func looksLikeBody(val string) bool {
	if strings.ContainsAny(val, "\n\r") {
		return true
	}
	return utf8.RuneCountInString(val) > argMaxRunes
}

// mcpToolParts splits an MCP tool name — `mcp__<server>__<tool>` — into the
// service and the operation. Both halves are identifiers the workspace
// configured, not anything the agent composed, so naming them is safe and is
// the only way "正在调用 calendar · list_events" beats "正在使用某个工具".
func mcpToolParts(tool string) (server, name string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(tool), "mcp__")
	if !found {
		return "", "", false
	}
	server, name, _ = strings.Cut(rest, "__")
	server, name = safeFragment(server), safeFragment(name)
	if server == "" {
		return "", "", false
	}
	if name == "" {
		name = server
	}
	return server, name, true
}

// safeFragment makes a fragment fit to be pasted into the bubble: one line, no
// control characters, no markup that could break out of the <think> wrapper
// the body is built from, and short enough to stay one line on a phone.
func safeFragment(s string) string {
	// Before anything else: a fragment is usually a command line, and a
	// command line is where a credential ends up in a chat that has no unsend
	// (progress_secrets.go).
	s = redactSecrets(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '<' || r == '>' || r == '&':
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > progressFragmentRunes {
		s = string(runes[:progressFragmentRunes]) + "…"
	}
	return s
}

// safeThinking cleans one increment of the agent's reasoning.
//
// It is deliberately gentler than safeFragment in one way and stricter in
// another. Gentler: line breaks survive, because this is a paragraph and not a
// step, and folding a page of reasoning onto one line is a way of not showing
// it. No trimming either — increments are cut wherever 500ms fell, so trimming
// each one would weld the last word of one to the first word of the next.
//
// Stricter: this is the only text in the frame the agent wrote freely, and the
// frame is a <think> block. A run reasoning about this very feature writes the
// literal, and a literal </think> would close the wrapper early and spill the
// rest of the bubble into the reply as if the agent had said it. So the tag is
// defused the same way the closing frame defuses it in an answer — a
// zero-width space after the angle bracket, which stops the client's scanner
// and leaves the reader the characters they would have seen (ws_frame.go).
func safeThinking(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
	return defuseThinkTags(redactSecrets(s))
}

// tailRunes keeps the last n runes of s, marking that something was dropped.
// Cutting by rune rather than byte is what keeps a multi-byte character from
// being sliced in half; the ellipsis is what stops the tail from reading as if
// the agent had begun mid-sentence.
func tailRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return "…" + string(runes[len(runes)-n:])
}

// ---- the rolling feed ----

// progressLine is one step as the user reads it, with how many times in a row
// it happened. Twenty greps in a row are one line and a count, not twenty
// lines that push everything else out of the window.
type progressLine struct {
	text  string
	count int
}

// progressFeed is one bubble's scrolling list of steps, plus the throttle that
// decides when the list is worth another frame. One per open bubble, owned by
// the stream store so it dies exactly when the bubble does.
type progressFeed struct {
	mu        sync.Mutex
	lines     []progressLine
	lastFlush time.Time
	now       func() time.Time

	// thinking is the tail of everything the agent has reasoned so far,
	// already cleaned and capped at progressThinkingRunes. It accumulates
	// rather than replacing, because the transcript delivers it as 500ms
	// increments of one continuous stream.
	thinking string
}

func newProgressFeed(now func() time.Time) *progressFeed {
	if now == nil {
		now = time.Now
	}
	return &progressFeed{now: now}
}

// record folds one step into the feed and returns the bubble body to send —
// or "" when this step merges into a frame that has already gone out inside
// the throttle window. A merged step is not lost: it is in the list, and the
// next frame to go out carries it.
//
// openedAt is when the user asked, which is what the elapsed clock counts
// from — not when the first step happened.
func (f *progressFeed) record(step progressStep, c copyPack, openedAt time.Time, level progressLevel) string {
	// Reasoning keeps its own edges; everything else is trimmed. A reasoning
	// increment is cut wherever the 500ms flush happened to fall, so its
	// leading and trailing spaces are load-bearing: they are the spaces
	// between words. Trimming each piece before joining them welds the last
	// word of one to the first word of the next ("先看 handler" + "再看 router"
	// becomes "先看 handler再看 router"), and eats the blank line a provider
	// puts in front of a new reasoning block to keep it apart from the last
	// one. safeThinking already says it does not trim, for exactly this
	// reason; this is the caller that was undoing it.
	text := step.line(c, level)
	if step.kind != progressThinking {
		text = strings.TrimSpace(text)
	}
	if strings.TrimSpace(text) == "" {
		return ""
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case step.kind == progressThinking:
		// One continuous stream arriving in pieces, so it appends and keeps
		// its tail. Folding it into the step list instead would push eight
		// increments of reasoning through the window and leave no room for
		// anything the agent actually did.
		f.thinking = tailRunes(f.thinking+text, progressThinkingRunes)
	case len(f.lines) > 0 && f.lines[len(f.lines)-1].text == text:
		f.lines[len(f.lines)-1].count++
	default:
		f.lines = append(f.lines, progressLine{text: text, count: 1})
		if len(f.lines) > progressMaxLines {
			f.lines = append(f.lines[:0], f.lines[len(f.lines)-progressMaxLines:]...)
		}
	}

	now := f.now()
	if !f.lastFlush.IsZero() && now.Sub(f.lastFlush) < progressMinInterval {
		return ""
	}
	f.lastFlush = now
	return f.renderLocked(c, now.Sub(openedAt))
}

// renderLocked builds the bubble body. Everything sits inside <think> tags so
// the client renders it as its own thinking affordance rather than as the
// bot's answer — a run that dies before chat:done would otherwise leave a list
// of half-finished steps looking like a reply. Caller holds f.mu.
//
// The order is steps, then reasoning, then the clock, and it is chosen so the
// top of the bubble holds still. The step list is bounded at eight lines and
// is the part a glance answers "is it doing what I asked" from; the reasoning
// is up to 1200 runes and grows, so putting it first would push the steps and
// the clock off the first screen every time the agent thought some more. The
// clock stays last because it is the one line that is always worth seeing and
// clients that preview a collapsed block preview its end.
func (f *progressFeed) renderLocked(c copyPack, elapsed time.Duration) string {
	var b strings.Builder
	b.WriteString("<think>")
	b.WriteString(strings.TrimSpace(c.StreamProgressPrefix))
	for _, l := range f.lines {
		b.WriteString("\n· ")
		b.WriteString(l.text)
		if l.count > 1 {
			b.WriteString(" ×")
			b.WriteString(strconv.Itoa(l.count))
		}
	}
	if thought := tidyThinking(f.thinking); thought != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(c.Progress.Thinking))
		b.WriteString("\n")
		b.WriteString(thought)
	}
	if elapsed >= time.Second {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf(c.Progress.Elapsed, formatElapsed(elapsed)))
	}
	b.WriteString("</think>")
	return b.String()
}

// tidyThinking makes the accumulated tail presentable at the moment it is
// rendered rather than as each increment arrives — the increments are cut
// wherever the batching fell, so a blank line can be split across two of them
// and only the whole is worth looking at.
// It also defuses the accumulated buffer one last time, which is the only
// place that can. safeThinking neutralises a `</think>` on the way in, but it
// sees one flush at a time: reasoning arriving as `</th` and then `ink>` has
// each half pass untouched and becomes a live closing tag once record() joins
// them. The bubble is one string wrapped in `<think>…</think>`, so a closing
// tag in the middle folds the panel early and drops the rest of the reasoning
// into the chat as the bot's answer.
//
// Here rather than in record() because the buffer is rune-capped as it is
// appended: defusing before the cap risks the zero-width space being exactly
// what gets sliced off, which would put the tag back together again.
func tidyThinking(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return defuseThinkTags(strings.TrimSpace(s))
}

// formatElapsed renders a duration the same way the web chat's status pill
// does (packages/views/chat/lib/format.ts), so the same run reads the same on
// both surfaces. Digits and unit letters carry across languages, which is why
// this needs no copy of its own.
func formatElapsed(d time.Duration) string {
	secs := int(d / time.Second)
	if secs < 0 {
		secs = 0
	}
	if secs < 60 {
		return strconv.Itoa(secs) + "s"
	}
	m, s := secs/60, secs%60
	if s == 0 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m " + strconv.Itoa(s) + "s"
}
