package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outbound_progress.go — what the agent is doing, shown while it does it.
//
// An agent writes a sentence and then works in tools for minutes, or starts
// with a tool call and writes nothing until the end. Streaming only its text
// leaves the chat showing one line, or nothing, for the whole run. So the tool
// calls go into the streamed reply as well: a short list of steps in a quote
// block under the text, edited in place as the run moves on, with a clock that
// keeps ticking between steps. When the agent writes again the list gives way
// to the text, and the final answer replaces the whole message.
//
// What a tool call is and which of its arguments may be shown is the WeCom
// adapter's vocabulary (wecom/progress_export.go): one tool table and one
// redactor for both channels.

// ProgressMode is how much of a run's tool calls a chat is shown.
type ProgressMode string

const (
	// ProgressAuto shows a private chat each step with its argument, and a
	// group the kind of work only: a group is read by people the run does not
	// belong to.
	ProgressAuto ProgressMode = "auto"
	// ProgressOff streams the agent's text and nothing else.
	ProgressOff ProgressMode = "off"
	// ProgressSummary shows the kind of work everywhere.
	ProgressSummary ProgressMode = "summary"
	// ProgressDetail shows each step with its argument everywhere.
	ProgressDetail ProgressMode = "detail"
)

// ParseProgressMode reads a configured mode. Empty is the default; an unknown
// value is the default too, and reported as such.
func ParseProgressMode(raw string) (ProgressMode, bool) {
	switch mode := ProgressMode(strings.ToLower(strings.TrimSpace(raw))); mode {
	case "":
		return ProgressAuto, true
	case ProgressAuto, ProgressOff, ProgressSummary, ProgressDetail:
		return mode, true
	}
	return ProgressAuto, false
}

// SetProgressMode chooses how much of a run's tool calls chats are shown. Call
// it before Register.
func (o *Outbound) SetProgressMode(mode ProgressMode) { o.progress = mode }

const (
	// maxStepLines is how many steps stay on screen. The list is where the run
	// is now, not its history.
	maxStepLines = 6
	// maxStepRunes keeps a step on one or two lines of a phone.
	maxStepRunes = 96
)

var stepIcons = map[wecom.StepKind]string{
	wecom.StepStarting: "⏳",
	wecom.StepRead:     "📖",
	wecom.StepEdit:     "✏️",
	wecom.StepCommand:  "💻",
	wecom.StepSearch:   "🔎",
	wecom.StepWeb:      "🌐",
	wecom.StepSubtask:  "🤖",
	wecom.StepPlan:     "📝",
	wecom.StepService:  "🔌",
	wecom.StepSkill:    "🧩",
	wecom.StepTool:     "🛠",
	wecom.StepError:    "⚠️",
}

// stepLine is one line of the list.
type stepLine struct {
	icon string
	text string
	// count is how many times in a row the step was taken.
	count int
	// transient marks a line that stands in until the first real step.
	transient bool
}

// stepFeed is the list under the text: the newest steps, and how many the run
// has taken in all.
type stepFeed struct {
	lines []stepLine
	total int
}

func (f *stepFeed) add(line stepLine) {
	if n := len(f.lines); n > 0 && f.lines[n-1].transient {
		f.lines = f.lines[:n-1]
	}
	if !line.transient {
		f.total++
	}
	if n := len(f.lines); n > 0 && f.lines[n-1].icon == line.icon && f.lines[n-1].text == line.text {
		// An agent polling a build repeats one call many times over. One line
		// that counts says so; six identical lines push everything else out.
		f.lines[n-1].count++
		return
	}
	line.count = 1
	f.lines = append(f.lines, line)
	if len(f.lines) > maxStepLines {
		f.lines = append(f.lines[:0], f.lines[len(f.lines)-maxStepLines:]...)
	}
}

// render is the quote block, or nothing when no step is showing. elapsed is
// how long the run has taken; it goes on the last line once there is a second
// to show, so the block changes while a step runs.
func (f *stepFeed) render(elapsed time.Duration) string {
	if len(f.lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<blockquote>")
	for i, line := range f.lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line.icon)
		b.WriteString(" ")
		b.WriteString(html.EscapeString(line.text))
		if line.count > 1 {
			b.WriteString(" ×")
			b.WriteString(strconv.Itoa(line.count))
		}
	}
	if elapsed >= time.Second {
		b.WriteString("\n⏱ ")
		b.WriteString(wecom.FormatElapsed(elapsed))
		switch {
		case f.total == 1:
			b.WriteString(" · 1 step")
		case f.total > 1:
			b.WriteString(" · ")
			b.WriteString(strconv.Itoa(f.total))
			b.WriteString(" steps")
		}
	}
	b.WriteString("</blockquote>")
	return b.String()
}

// streamFrame is one thing a run adds to its reply: text, a step, or neither —
// the deferred push, which only sends what is already kept.
type streamFrame struct {
	text string
	step *stepLine
}

func (f streamFrame) empty() bool { return f.text == "" && f.step == nil }

// absorb keeps a frame in the stream. Caller holds Outbound.mu.
func (st *streamState) absorb(frame streamFrame, now time.Time) {
	if frame.empty() {
		return
	}
	st.lastFrameAt = now
	if frame.text != "" {
		if len(st.steps.lines) > 0 {
			// The agent is writing again: what it was doing is over. Text on
			// either side of a tool call is two separate remarks, which the
			// agent sends without anything between them.
			st.steps.lines = nil
			if st.accumulated != "" && !strings.HasSuffix(st.accumulated, "\n") && !strings.HasPrefix(frame.text, "\n") {
				st.accumulated += "\n\n"
			}
		}
		st.accumulated += frame.text
	}
	if frame.step != nil {
		st.steps.add(*frame.step)
	}
}

// render is the message body for what the stream holds now: the text, then the
// steps. Empty when there is neither. Caller holds Outbound.mu.
func (st *streamState) render(now time.Time) string {
	block := st.steps.render(now.Sub(st.startedAt))
	budget := maxMessageUnits
	if block != "" {
		budget -= utf16Units(block) + 2
	}
	text := st.accumulated
	if utf16Units(text) > budget {
		// Mid-stream overflow: freeze the streamed message at the cap; the full
		// reply is delivered in chunks by the final EventChatDone send.
		text = chunkMessage(text, budget)[0]
	}
	body := formatHTML(text)
	switch {
	case block == "":
		return body
	case body == "":
		return block
	}
	return body + "\n\n" + block
}

// stepFor words a tool call or an error as a line of the list, for the chat it
// is going to.
func (o *Outbound) stepFor(target *replyTarget, payload protocol.TaskMessagePayload) (stepLine, bool) {
	detail := o.progress == ProgressDetail || (o.progress == ProgressAuto && target.private)
	if detail {
		payload.Input = unwrapShellCommand(payload.Input)
	}
	step, ok := wecom.DescribeStep(payload, wecom.LocaleEn, detail)
	if !ok {
		return stepLine{}, false
	}
	return stepLine{icon: stepIcons[step.Kind], text: clipMiddle(step.Text, maxStepRunes)}, true
}

// shellWrapper matches a command handed to a shell to run: `bash -lc "…"`,
// `/bin/zsh -c '…'`.
var shellWrapper = regexp.MustCompile(`(?s)^\s*(?:\S*/)?(?:ba|z|da|k)?sh\s+-[a-z]*c\s+(.+?)\s*$`)

// unwrapShellCommand returns the input with the wrapper shell taken off its
// command. Half of all commands arrive as `/bin/bash -lc "git status"`, and
// the wrapper is the same every time: shown as it is, it fills the line and
// pushes the part that says what is running off the end. The input is copied;
// other subscribers read the same map.
func unwrapShellCommand(input map[string]any) map[string]any {
	for _, key := range []string{"command", "cmd"} {
		var inner string
		switch v := input[key].(type) {
		case string:
			inner = scriptHead(unwrapShell(v))
			if inner == v {
				return input
			}
		case []any:
			// The argv form: ["bash", "-lc", "git status"].
			parts := make([]string, 0, len(v))
			for _, part := range v {
				s, ok := part.(string)
				if !ok {
					return input
				}
				parts = append(parts, s)
			}
			if len(parts) == 0 {
				return input
			}
			inner = strings.Join(parts, " ")
			if len(parts) == 3 && shellWrapper.MatchString(parts[0]+" "+parts[1]+" x") {
				inner = parts[2]
			}
			inner = scriptHead(inner)
		default:
			continue
		}
		out := make(map[string]any, len(input))
		for k, v := range input {
			out[k] = v
		}
		out[key] = inner
		return out
	}
	return input
}

func unwrapShell(command string) string {
	m := shellWrapper.FindStringSubmatch(command)
	if m == nil {
		return command
	}
	inner := m[1]
	if len(inner) < 2 {
		return inner
	}
	switch quote := inner[0]; {
	case quote == '"' && inner[len(inner)-1] == '"':
		inner = inner[1 : len(inner)-1]
		inner = shellEscapes.Replace(inner)
	case quote == '\'' && inner[len(inner)-1] == '\'':
		inner = strings.ReplaceAll(inner[1:len(inner)-1], `'\''`, `'`)
	}
	return inner
}

// scriptHead reduces a script to its first line. An agent often sends several
// commands, or a patch in a here-document, as one command: the first line says
// where it starts, and the rest would put file contents into the chat.
func scriptHead(command string) string {
	lines := strings.Split(strings.TrimSpace(command), "\n")
	head := strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		return head
	}
	return head + " …"
}

// shellEscapes are the characters a backslash protects inside double quotes.
var shellEscapes = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`")

// clipMiddle shortens a line by cutting its middle. A command's start says
// what it is and its end says what it is aimed at; the middle is flags.
func clipMiddle(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	head := limit * 2 / 3
	tail := limit - head - 1
	return strings.TrimRight(string(runes[:head]), " ") + "…" + strings.TrimLeft(string(runes[len(runes)-tail:]), " ")
}

// heartbeatInterval is how long to wait before moving the clock under a list
// of steps, given how long the run has been quiet. A private chat takes an
// edit a second and a group twenty a minute, so the clock moves often at first
// and slows for a step that turns out to be long; a run quiet for hours is
// left alone, since the final answer does not need the clock to arrive.
func heartbeatInterval(chatID int64, quiet time.Duration) time.Duration {
	switch {
	case quiet >= 3*time.Hour:
		return 0
	case quiet >= 10*time.Minute:
		return time.Minute
	case chatID < 0:
		if quiet >= 2*time.Minute {
			return 20 * time.Second
		}
		return 10 * time.Second
	case quiet >= 2*time.Minute:
		return 10 * time.Second
	}
	return 3 * time.Second
}

// handleTaskProgress opens the reply the moment the agent is launched. The
// first frame of a run can be many seconds away — the CLI starting, the model
// thinking — and until then the chat would show nothing at all.
func (o *Outbound) handleTaskProgress(e events.Event) {
	payload, ok := e.Payload.(protocol.TaskProgressPayload)
	if !ok || payload.Step != 1 {
		// The daemon's other milestone is the run finishing, and the answer is
		// already on its way then.
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, turn, ok := o.streamTarget(ctx, e)
	if !ok {
		return
	}
	starting := wecom.StartingStep(wecom.LocaleEn)
	o.advanceStream(ctx, target, turn, streamFrame{step: &stepLine{
		icon: stepIcons[starting.Kind], text: starting.Text, transient: true,
	}})
}

// streamRemnant is what a stream left in the chat when its run ended without
// an answer to replace it.
type streamRemnant struct {
	chatID         int64
	messageID      int64
	installationID pgtype.UUID
	// body is the message without its steps; empty when steps were all it had.
	body string
}

// takeStream ends the task's stream and reports what has to be tidied in the
// chat: a list of steps that no answer is going to replace would say "running"
// for good.
func (o *Outbound) takeStream(e events.Event) *streamRemnant {
	taskID, ok := eventTaskID(e)
	if !ok {
		return nil
	}
	key := util.UUIDToString(taskID)
	var remnant *streamRemnant
	o.mu.Lock()
	if st := o.streams[key]; st != nil {
		// A request already on its way to Telegram finds this when it lands.
		st.ended = true
		remnant = o.remnantLocked(st)
	}
	o.mu.Unlock()
	o.clearStreamKey(key)
	return remnant
}

// remnantLocked keeps the accumulated prose without steps, including text
// waiting for its edit slot. The caller holds o.mu.
func (o *Outbound) remnantLocked(st *streamState) *streamRemnant {
	if st.messageID == 0 || st.target == nil {
		return nil
	}
	st.steps.lines = nil
	return &streamRemnant{
		chatID: st.chatID, messageID: st.messageID, installationID: st.target.installationID,
		body: st.render(o.now()),
	}
}

// recoverStreamRemnant uses the shared delivery and persisted transcript when
// the closing replica never saw the stream. The settled reason/depth check also
// prevents a late cancellation from replacing a final answer or a newer retry.
func (o *Outbound) recoverStreamRemnant(ctx context.Context, target *replyTarget, turn replyTurn, reason string, local *streamRemnant) (*streamRemnant, error) {
	row, err := o.q.GetChannelReplyDelivery(ctx, turn.id)
	if err != nil {
		return nil, fmt.Errorf("load telegram reply for cleanup: %w", err)
	}
	messageID := (&deliveryLease{row: row}).messageID()
	if row.Phase != deliveryPhaseSettled || row.SettledReason != reason ||
		turn.depth < row.AttemptDepth || row.ChunksSent != 0 || messageID == 0 {
		return nil, nil
	}
	fragments, err := o.q.ListTaskReplyFragments(ctx, row.TaskID)
	if err != nil {
		return nil, fmt.Errorf("load telegram reply text for cleanup: %w", err)
	}
	if len(fragments) == 0 && local != nil && local.messageID == messageID {
		return local, nil
	}
	var text streamState
	for _, fragment := range fragments {
		if fragment.Type == "text" {
			text.absorb(streamFrame{text: fragment.Content}, o.now())
		} else if o.progress != ProgressOff {
			text.absorb(streamFrame{step: &stepLine{}}, o.now())
		}
	}
	text.steps.lines = nil
	return &streamRemnant{
		chatID: target.chatID, messageID: messageID, installationID: target.installationID,
		body: text.render(o.now()),
	}, nil
}

// tidyRemnant takes the steps off a message whose run is over: the text stays
// as the agent left it, and a message that was only steps is removed. The turn
// is closed by now, so nothing else writes to the message.
func (o *Outbound) tidyRemnant(ctx context.Context, remnant *streamRemnant) {
	if remnant == nil {
		return
	}
	call := func() error {
		ctx, cancel := o.callContext(ctx)
		defer cancel()
		token, err := o.activeBotToken(ctx, remnant.installationID)
		if err != nil || token == "" {
			return err
		}
		api := newBotAPI(o.apiBase, token, o.client)
		if remnant.body == "" {
			return api.DeleteMessage(ctx, remnant.chatID, remnant.messageID)
		}
		return api.EditMessageText(ctx, editMessageTextParams{
			ChatID: remnant.chatID, MessageID: remnant.messageID, Text: remnant.body, ParseMode: "HTML",
		})
	}
	err := call()
	if retry, ok := retryAfter(err); ok && retry <= remnantRetryLimit {
		if o.wait(ctx, retry) != nil {
			return
		}
		err = call()
	}
	if err != nil && !isNotModified(err) && !isMessageGone(err) {
		o.logger.WarnContext(ctx, "telegram outbound: could not take the steps off a finished reply", "error", err)
	}
}

// remnantRetryLimit is the longest retry_after worth waiting out to tidy a
// message; past it the steps stay, which is untidy and nothing worse.
const remnantRetryLimit = 5 * time.Second

// isMessageGone reports that the message to tidy is not there any more — the
// user deleted it, which leaves nothing to tidy.
func isMessageGone(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == http.StatusBadRequest &&
		(containsFold(ae.Description, "message to edit not found") ||
			containsFold(ae.Description, "message to delete not found"))
}
