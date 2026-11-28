package lark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testBotOpenID  = "ou_bot"
	testUserOpenID = "ou_alice"
)

// docCommentFake adds the comment transport to any APIClient fake.
type docCommentFake struct {
	APIClient
	mu         sync.Mutex
	comment    DocComment
	commentErr error
	pages      []DocCommentReplyPage
	listTokens []string
	meta       DocMeta
	metaErr    error
	replyErrs  []error
	replies    []docCommentPost
	comments   []docCommentPost
	reactions  []string
	removed    []string
}

type docCommentPost struct {
	FileToken, FileType, CommentID string
	Elements                       []DocCommentElement
}

func (f *docCommentFake) GetDocComment(_ context.Context, _ InstallationCredentials, _, _, _ string) (DocComment, error) {
	return f.comment, f.commentErr
}

func (f *docCommentFake) ListDocCommentReplies(_ context.Context, _ InstallationCredentials, _, _, _, pageToken string) (DocCommentReplyPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listTokens = append(f.listTokens, pageToken)
	if len(f.pages) == 0 {
		return DocCommentReplyPage{}, errors.New("no more pages")
	}
	p := f.pages[0]
	f.pages = f.pages[1:]
	return p, nil
}

func (f *docCommentFake) GetDocMeta(context.Context, InstallationCredentials, string, string) (DocMeta, error) {
	return f.meta, f.metaErr
}

func (f *docCommentFake) CreateDocCommentReply(_ context.Context, _ InstallationCredentials, fileToken, fileType, commentID string, elements []DocCommentElement) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, docCommentPost{fileToken, fileType, commentID, elements})
	if len(f.replyErrs) > 0 {
		err := f.replyErrs[0]
		f.replyErrs = f.replyErrs[1:]
		if err != nil {
			return "", err
		}
	}
	return "r_new", nil
}

func (f *docCommentFake) CreateDocComment(_ context.Context, _ InstallationCredentials, fileToken, fileType string, elements []DocCommentElement) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, docCommentPost{fileToken, fileType, "", elements})
	return "c_new", nil
}

func (f *docCommentFake) AddDocCommentReaction(_ context.Context, _ InstallationCredentials, _, _, replyID, reactionType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions = append(f.reactions, replyID+":"+reactionType)
	return nil
}

func (f *docCommentFake) RemoveDocCommentReaction(_ context.Context, _ InstallationCredentials, _, _, replyID, reactionType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, replyID+":"+reactionType)
	return nil
}

func textEl(s string) DocCommentElement {
	return DocCommentElement{Type: "text_run", TextRun: &DocCommentTextRun{Text: s}}
}

func personEl(id string) DocCommentElement {
	return DocCommentElement{Type: "person", Person: &DocCommentPersonRef{UserID: id}}
}

func reply(id, user string, els ...DocCommentElement) DocCommentReply {
	r := DocCommentReply{ReplyID: id, UserID: user}
	r.Content.Elements = els
	return r
}

func commentWith(id string, whole bool, quote string, replies ...DocCommentReply) DocComment {
	c := DocComment{CommentID: id, IsWhole: whole, Quote: quote}
	c.ReplyList.Replies = replies
	return c
}

func decodeDocCommentFrame(t *testing.T, event string) (InboundMessage, bool, error) {
	t.Helper()
	payload := `{"schema":"2.0","header":{"event_id":"ev_1","event_type":"drive.notice.comment_add_v1","create_time":"1700000000000","app_id":"cli_app"},"event":` + event + `}`
	return NewLarkJSONFrameDecoder().Decode([]byte(payload), Installation{AppID: "cli_app", BotOpenID: testBotOpenID})
}

func TestDecodeDocCommentEvent(t *testing.T) {
	documented := `{"notice_meta":{"file_type":"docx","file_token":"doxTok","from_user_id":{"open_id":"ou_alice","union_id":"on_a"},"to_user_id":{"open_id":"ou_bot"},"notice_type":"add_comment"},"comment_id":"c1","reply_id":"r1","is_mentioned":true}`
	swapped := `{"file_token":"doxTok","file_type":"docx","comment_id":"c1","reply_id":"r1","notice_meta":{"is_mentioned":true,"notice_type":"add_comment","from_user_id":{"open_id":"ou_alice"}}}`
	stringMeta := `{"comment_id":"c1","reply_id":"r1","is_mentioned":true,"notice_meta":"{\"file_token\":\"doxTok\",\"file_type\":\"docx\",\"from_user_id\":{\"open_id\":\"ou_alice\"},\"to_user_id\":{\"open_id\":\"ou_bot\"}}"}`
	for name, event := range map[string]string{"documented": documented, "swapped": swapped, "string notice_meta": stringMeta} {
		t.Run(name, func(t *testing.T) {
			msg, ok, err := decodeDocCommentFrame(t, event)
			if err != nil || !ok {
				t.Fatalf("decode: ok=%v err=%v", ok, err)
			}
			if msg.ChatID != "doc_comment:doxTok:c1" || msg.ChatType != ChatTypeGroup || msg.MessageID != "r1" ||
				msg.SenderOpenID != testUserOpenID || msg.AppID != "cli_app" || msg.EventID != "ev_1" || !msg.AddressedToBot {
				t.Fatalf("unexpected message: %+v", msg)
			}
			if msg.DocComment == nil || msg.DocComment.FileToken != "doxTok" || msg.DocComment.FileType != "docx" ||
				msg.DocComment.CommentID != "c1" || msg.DocComment.ReplyID != "r1" || msg.DocComment.botOpenID != testBotOpenID {
				t.Fatalf("unexpected ref: %+v", msg.DocComment)
			}
		})
	}

	t.Run("own reply is ignored", func(t *testing.T) {
		_, ok, err := decodeDocCommentFrame(t, `{"notice_meta":{"file_type":"docx","file_token":"doxTok","from_user_id":{"open_id":"ou_bot"}},"comment_id":"c1","reply_id":"r2"}`)
		if ok || err != nil {
			t.Fatalf("own reply must be dropped quietly: ok=%v err=%v", ok, err)
		}
	})
	t.Run("missing ids", func(t *testing.T) {
		if _, ok, err := decodeDocCommentFrame(t, `{"notice_meta":{"file_type":"docx","from_user_id":{"open_id":"ou_alice"}},"comment_id":"c1","reply_id":"r1"}`); ok || err == nil {
			t.Fatalf("missing file token must be a decode error: ok=%v err=%v", ok, err)
		}
	})
	t.Run("mention addressed to another bot", func(t *testing.T) {
		msg, ok, err := decodeDocCommentFrame(t, `{"notice_meta":{"file_type":"docx","file_token":"doxTok","from_user_id":{"open_id":"ou_alice"},"to_user_id":{"open_id":"ou_other_bot"}},"comment_id":"c1","reply_id":"r1","is_mentioned":true}`)
		if err != nil || !ok || msg.AddressedToBot {
			t.Fatalf("a notice for another bot must not address this one: ok=%v err=%v addressed=%v", ok, err, msg.AddressedToBot)
		}
	})
}

func newDocCommentEnricher(fake *docCommentFake) *inboundEnricher {
	return NewInboundEnricher(fake, InboundEnricherConfig{RecentContextSize: 5, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).(*inboundEnricher)
}

func decodedDocComment(t *testing.T, mentioned bool) InboundMessage {
	t.Helper()
	event := `{"notice_meta":{"file_type":"docx","file_token":"doxTok","from_user_id":{"open_id":"ou_alice"},"to_user_id":{"open_id":"ou_bot"}},"comment_id":"c1","reply_id":"r3","is_mentioned":` + map[bool]string{true: "true", false: "false"}[mentioned] + `}`
	msg, ok, err := decodeDocCommentFrame(t, event)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	return msg
}

func TestEnrichDocCommentAnchoredThread(t *testing.T) {
	base := newEnricherFake()
	base.userNames = map[string]string{testUserOpenID: "Alice", "ou_bob": "Bob"}
	fake := &docCommentFake{
		APIClient: base,
		meta:      DocMeta{Title: "Spec", URL: "https://example.feishu.cn/docx/doxTok"},
		comment: commentWith("c1", false, "the selected sentence",
			reply("r1", "ou_bob", personEl(testBotOpenID), textEl(" is this right?")),
			reply("r2", testUserOpenID, textEl("not sure")),
			reply("r3", testUserOpenID, personEl(testBotOpenID), textEl(" please check with "), personEl("ou_bob")),
		),
	}
	msg := newDocCommentEnricher(fake).Enrich(context.Background(), decodedDocComment(t, true), InstallationCredentials{})

	if !msg.AddressedToBot || msg.ChatID != "doc_comment:doxTok:c1" || msg.DocComment.Whole {
		t.Fatalf("unexpected routing: addressed=%v chat=%q ref=%+v", msg.AddressedToBot, msg.ChatID, msg.DocComment)
	}
	if msg.CommandBody != "please check with @Bob" {
		t.Fatalf("command body = %q", msg.CommandBody)
	}
	for _, want := range []string{
		`<feishu_doc_comment file_type="docx" file_token="doxTok" comment_id="c1" comment_kind="anchored" title="Spec" url="https://example.feishu.cn/docx/doxTok">`,
		"<quoted_text>\nthe selected sentence\n</quoted_text>",
		"<comment_thread count=\"2\">\n[Bob]: @Bot is this right?\n[Alice]: not sure\n</comment_thread>",
		"</feishu_doc_comment>\n\n[Alice]: please check with @Bob",
	} {
		if !strings.Contains(msg.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, msg.Body)
		}
	}
}

func TestEnrichDocCommentAddressing(t *testing.T) {
	cases := []struct {
		name    string
		earlier []DocCommentReply
		want    bool
	}{
		{"follow-up in a thread the bot answered", []DocCommentReply{reply("r1", testUserOpenID, textEl("q")), reply("r2", testBotOpenID, textEl("a"))}, true},
		{"unmentioned reply between people", []DocCommentReply{reply("r1", "ou_bob", textEl("q")), reply("r2", "ou_bob", textEl("a"))}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replies := append(tc.earlier, reply("r3", testUserOpenID, textEl("and the second part?")))
			fake := &docCommentFake{APIClient: newEnricherFake(), comment: commentWith("c1", false, "q", replies...)}
			msg := newDocCommentEnricher(fake).Enrich(context.Background(), decodedDocComment(t, false), InstallationCredentials{})
			if msg.AddressedToBot != tc.want {
				t.Fatalf("addressed = %v, want %v", msg.AddressedToBot, tc.want)
			}
		})
	}
}

func TestEnrichDocCommentWholeDocument(t *testing.T) {
	fake := &docCommentFake{
		APIClient: newEnricherFake(),
		comment:   commentWith("c1", true, "", reply("r3", testUserOpenID, personEl(testBotOpenID), textEl(" /issue Fix the intro\nIt repeats  the summary."))),
	}
	msg := newDocCommentEnricher(fake).Enrich(context.Background(), decodedDocComment(t, true), InstallationCredentials{})
	if !msg.DocComment.Whole || msg.ChatID != "doc_comment:doxTok" {
		t.Fatalf("whole-document comments share one session per file: chat=%q ref=%+v", msg.ChatID, msg.DocComment)
	}
	if route := msg.DocComment.route(); route.CommentID != "" {
		t.Fatalf("whole-document route must answer with a new comment, got %+v", route)
	}
	if msg.CommandBody != "/issue Fix the intro\nIt repeats  the summary." {
		t.Fatalf("commands are read from the user's own text, line breaks kept, got %q", msg.CommandBody)
	}
}

func TestEnrichDocCommentFetchFailureKeepsEventVerdict(t *testing.T) {
	fake := &docCommentFake{APIClient: newEnricherFake(), commentErr: errors.New("forbidden"), metaErr: errors.New("forbidden")}
	msg := newDocCommentEnricher(fake).Enrich(context.Background(), decodedDocComment(t, true), InstallationCredentials{})
	if !msg.AddressedToBot || !strings.Contains(msg.Body, docCommentUnavailableLine) || !strings.Contains(msg.Body, `file_token="doxTok"`) || !strings.Contains(msg.Body, "</feishu_doc_comment>") {
		t.Fatalf("a failed fetch must still reach the agent with the document reference: addressed=%v body=%q", msg.AddressedToBot, msg.Body)
	}
}

func TestEnrichDocCommentPagesLongThread(t *testing.T) {
	first := commentWith("c1", false, "q", reply("r1", "ou_bob", textEl("first")))
	first.HasMore = true
	fake := &docCommentFake{
		APIClient: newEnricherFake(),
		comment:   first,
		pages: []DocCommentReplyPage{
			{Items: []DocCommentReply{reply("r1", "ou_bob", textEl("first")), reply("r2", "ou_bob", textEl("second"))}, HasMore: true, PageToken: "p2"},
			{Items: []DocCommentReply{reply("r3", testUserOpenID, personEl(testBotOpenID), textEl(" summarize"))}},
		},
	}
	msg := newDocCommentEnricher(fake).Enrich(context.Background(), decodedDocComment(t, true), InstallationCredentials{})
	if msg.CommandBody != "summarize" || !strings.Contains(msg.Body, `<comment_thread count="2">`) {
		t.Fatalf("trigger beyond the first batch must be found by paging: command=%q body=%q", msg.CommandBody, msg.Body)
	}
	if len(fake.listTokens) != 2 || fake.listTokens[0] != "" || fake.listTokens[1] != "p2" {
		t.Fatalf("unexpected paging: %v", fake.listTokens)
	}
}

func TestLarkSessionRoutingDocComment(t *testing.T) {
	lm := InboundMessage{ChatID: "doc_comment:doxTok:c1", ChatType: ChatTypeGroup, DocComment: &DocCommentRef{FileToken: "doxTok", FileType: "sheet", CommentID: "c1", ReplyID: "r1"}}
	key, config := larkSessionRouting(channelMessageFromLark(lm))
	if key != "doc_comment:doxTok:c1" {
		t.Fatalf("key = %q", key)
	}
	route, ok := docCommentRouteFromConfig(config)
	if !ok || route != (docCommentRoute{FileToken: "doxTok", FileType: "sheet", CommentID: "c1"}) {
		t.Fatalf("route = %+v ok=%v (config %s)", route, ok, config)
	}
	if outboundChatID(ChatSessionBinding{ChannelChatID: key, Config: config}) != ChatID(key) {
		t.Fatalf("a comment route must not read as a topic-isolated IM chat")
	}
}

func newDocCommentPatcher(t *testing.T, route docCommentRoute) (*Patcher, *fakePatcherQueries, *docCommentFake) {
	t.Helper()
	p, q, api := newTestPatcher(t)
	fake := &docCommentFake{APIClient: api}
	p = NewPatcher(q, fakeCredentials{secret: "shh"}, fake, PatcherConfig{Logger: newDiscardLogger(), Now: time.Now})
	config, _ := json.Marshal(larkBindingConfig{DocComment: &route})
	q.binding.ChannelChatID = "doc_comment:doxTok:c1"
	q.binding.ChatType = string(ChatTypeGroup)
	q.binding.Config = config
	q.binding.LastMessageID.String, q.binding.LastMessageID.Valid = "r3", true
	q.binding.LastSenderID.String, q.binding.LastSenderID.Valid = testUserOpenID, true
	return p, q, fake
}

func chatDone(p *Patcher, q *fakePatcherQueries, content string) {
	taskID := "ee333333-ee33-ee33-ee33-eeeeeeeeeeee"
	p.handleEvent(events.Event{
		Type: protocol.EventChatDone, TaskID: taskID, ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: uuidString(q.binding.ChatSessionID), Content: content},
	})
}

func elementsText(els []DocCommentElement) string {
	var b strings.Builder
	for _, el := range els {
		switch el.Type {
		case "person":
			b.WriteString("@" + el.Person.UserID)
		case "text_run":
			b.WriteString(el.TextRun.Text)
		}
	}
	return b.String()
}

func TestPatcherAnswersDocCommentThread(t *testing.T) {
	p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "doxTok", FileType: "docx", CommentID: "c1"})
	chatDone(p, q, "Looks right: x < y & &lt;")

	if len(fake.replies) != 1 || len(fake.comments) != 0 {
		t.Fatalf("want one thread reply, got replies=%+v comments=%+v", fake.replies, fake.comments)
	}
	got := fake.replies[0]
	if got.FileToken != "doxTok" || got.FileType != "docx" || got.CommentID != "c1" {
		t.Fatalf("reply target = %+v", got)
	}
	if text := elementsText(got.Elements); text != "@ou_alice Looks right: x &lt; y &amp; &amp;lt;" {
		t.Fatalf("reply text = %q", text)
	}
	api := fake.APIClient.(*fakeAPIClient)
	if len(api.textSent) != 0 || len(api.mdCardSent) != 0 || len(api.sent) != 0 {
		t.Fatalf("a comment answer must not go to IM")
	}
}

func TestPatcherAnswersWholeDocCommentWithNewComment(t *testing.T) {
	p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "doxTok", FileType: "docx"})
	chatDone(p, q, "Done.")
	if len(fake.replies) != 0 || len(fake.comments) != 1 || elementsText(fake.comments[0].Elements) != "@ou_alice Done." {
		t.Fatalf("want one new whole-document comment, got replies=%+v comments=%+v", fake.replies, fake.comments)
	}
}

func TestPatcherDocCommentReplyFallsBackToNewComment(t *testing.T) {
	p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "doxTok", FileType: "docx", CommentID: "c1"})
	fake.replyErrs = []error{&APIError{Op: "reply to doc comment", Code: codeDocCommentParamError, Msg: "param error"}}
	chatDone(p, q, "answer")
	if len(fake.replies) != 1 || len(fake.comments) != 1 {
		t.Fatalf("a rejected reply must fall back to a new comment: replies=%d comments=%d", len(fake.replies), len(fake.comments))
	}
}

func TestPatcherDocCommentReplyDoesNotFallBackOnOtherErrors(t *testing.T) {
	p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "shtTok", FileType: "sheet", CommentID: "c1"})
	fake.replyErrs = []error{&APIError{Op: "reply to doc comment", Code: codeDocCommentParamError, Msg: "param error"}}
	chatDone(p, q, "answer")
	if len(fake.comments) != 0 {
		t.Fatalf("sheets cannot take a whole-document comment; got %+v", fake.comments)
	}
}

func TestPatcherDocCommentFailureNotice(t *testing.T) {
	p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "doxTok", FileType: "docx", CommentID: "c1"})
	taskID := "ee333333-ee33-ee33-ee33-eeeeeeeeeeee"
	p.handleEvent(events.Event{
		Type: protocol.EventTaskFailed, TaskID: taskID, ChatSessionID: uuidString(q.binding.ChatSessionID),
		Payload: map[string]any{"task_id": taskID, "error": "runtime offline"},
	})
	if len(fake.replies) != 1 || elementsText(fake.replies[0].Elements) != "@ou_alice TestAgent: Run failed: runtime offline" {
		t.Fatalf("failure notice = %+v", fake.replies)
	}
}

// The working-on-it reaction goes on when the run is scheduled and comes off
// when the run ends, whether it answered or was cancelled.
func TestDocCommentReactionLifecycle(t *testing.T) {
	for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskCancelled} {
		t.Run(eventType, func(t *testing.T) {
			p, q, fake := newDocCommentPatcher(t, docCommentRoute{FileToken: "doxTok", FileType: "docx", CommentID: "c1"})
			typing := NewTypingIndicatorManager(fake, fakeTypingCreds{secret: "shh"},
				&fakeTypingQueries{binding: q.binding, installation: q.installation}, newDiscardLogger())
			p.SetTypingIndicatorManager(typing)

			typing.AckDocComment(context.Background(), q.installation, DocCommentRef{FileToken: "doxTok", FileType: "docx", CommentID: "c1", ReplyID: "r3"})
			if len(fake.reactions) != 1 || fake.reactions[0] != "r3:Typing" {
				t.Fatalf("reaction added = %v", fake.reactions)
			}
			taskID := "ee333333-ee33-ee33-ee33-eeeeeeeeeeee"
			p.handleEvent(events.Event{
				Type: eventType, TaskID: taskID, ChatSessionID: uuidString(q.binding.ChatSessionID),
				Payload: protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: uuidString(q.binding.ChatSessionID), Content: "done"},
			})
			if len(fake.removed) != 1 || fake.removed[0] != "r3:Typing" {
				t.Fatalf("reaction removed = %v", fake.removed)
			}
		})
	}
}

func TestDocCommentReplyChunks(t *testing.T) {
	line := strings.Repeat("a", 99) + "\n"
	chunks := docCommentReplyChunks("ou_alice", strings.Repeat(line, 100)) // 10,000 runes
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(chunks))
	}
	if chunks[0][0].Type != "person" || len(chunks[1]) != 1 {
		t.Fatalf("only the first chunk mentions the asker")
	}
	for i, c := range chunks {
		text := c[len(c)-1].TextRun.Text
		if n := len([]rune(text)); n > docCommentChunkRunes || strings.HasSuffix(text, "\n") || strings.HasPrefix(text, "\n") {
			t.Fatalf("chunk %d: %d runes, must split on a line break", i, n)
		}
	}
}

func TestOutcomeReplierBindingFallbackInDocComment(t *testing.T) {
	stub := &stubAPIClientWithRecorder{configured: true, bindingErr: &APIError{Op: "send binding prompt", Code: codeNoAvailability}}
	fake := &docCommentFake{APIClient: stub}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   fake,
		BindingSvc:  fakeBindingMinter{raw: "tok"},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      newDiscardLogger(),
	})
	msg := InboundMessage{ChatID: "doc_comment:doxTok:c1", ChatType: ChatTypeGroup, SenderOpenID: testUserOpenID,
		DocComment: &DocCommentRef{FileToken: "doxTok", FileType: "docx", CommentID: "c1", ReplyID: "r3"}}
	rep.Reply(context.Background(), Installation{AppID: "cli_x"}, msg, DispatchResult{Outcome: OutcomeNeedsBinding, SenderOpenID: testUserOpenID})
	if len(fake.replies) != 1 || elementsText(fake.replies[0].Elements) != "@ou_alice "+bindingPromptUnavailableDocCommentCopy {
		t.Fatalf("an undeliverable binding card must be explained in the comment thread: %+v", fake.replies)
	}
}

func TestOutcomeReplierAnswersDocCommentInThread(t *testing.T) {
	stub := &stubAPIClientWithRecorder{configured: true}
	fake := &docCommentFake{APIClient: stub}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   fake,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	msg := InboundMessage{ChatID: "doc_comment:doxTok:c1", ChatType: ChatTypeGroup, MessageID: "r3", SenderOpenID: testUserOpenID,
		DocComment: &DocCommentRef{FileToken: "doxTok", FileType: "docx", CommentID: "c1", ReplyID: "r3"}}
	rep.Reply(context.Background(), Installation{AppID: "cli_x"}, msg, DispatchResult{Outcome: OutcomeAgentOffline})

	if len(fake.replies) != 1 || fake.replies[0].CommentID != "c1" || !strings.Contains(elementsText(fake.replies[0].Elements), agentOfflineCopy) {
		t.Fatalf("notice must go to the comment thread: %+v", fake.replies)
	}
	if len(stub.interactiveOut) != 0 || len(stub.textOut) != 0 {
		t.Fatalf("notice must not go to IM")
	}
}

func TestHTTPClientDocCommentRequests(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("t-doc", 7200)
	type seen struct {
		method, path, query string
		body                map[string]any
	}
	var calls []seen
	record := func(resp map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			calls = append(calls, seen{r.Method, r.URL.Path, r.URL.RawQuery, body})
			writeJSON(w, resp)
		}
	}
	fake.mux.HandleFunc("/open-apis/drive/v1/files/doxTok/comments/batch_query", record(map[string]any{"code": 0, "data": map[string]any{
		"items": []any{map[string]any{"comment_id": "c1", "is_whole": false, "quote": "q", "reply_list": map[string]any{"replies": []any{
			map[string]any{"reply_id": "r1", "user_id": "ou_alice", "content": map[string]any{"elements": []any{
				map[string]any{"type": "text_run", "text_run": map[string]any{"text": "hi"}, "person": map[string]any{"user_id": nil}},
			}}},
		}}}},
	}}))
	fake.mux.HandleFunc("/open-apis/drive/v1/files/doxTok/comments/c1/replies", record(map[string]any{"code": 0, "data": map[string]any{"reply_id": "r9", "items": []any{}, "has_more": false}}))
	fake.mux.HandleFunc("/open-apis/drive/v1/files/doxTok/comments", record(map[string]any{"code": 0, "data": map[string]any{"comment_id": "c9"}}))
	fake.mux.HandleFunc("/open-apis/drive/v2/files/doxTok/comments/reaction", record(map[string]any{"code": 0, "data": map[string]any{}}))
	fake.mux.HandleFunc("/open-apis/drive/v1/metas/batch_query", record(map[string]any{"code": 0, "data": map[string]any{
		"metas": []any{map[string]any{"doc_token": "doxTok", "title": "Spec", "url": "https://x.feishu.cn/docx/doxTok"}},
	}}))
	c := newTestClient(fake, time.Now)
	ctx := context.Background()
	creds := testCreds()

	comment, err := c.GetDocComment(ctx, creds, "doxTok", "docx", "c1")
	if err != nil || comment.Quote != "q" || len(comment.ReplyList.Replies) != 1 || renderDocCommentElements(comment.ReplyList.Replies[0].Content.Elements, "", nil) != "hi" {
		t.Fatalf("GetDocComment = %+v, %v", comment, err)
	}
	if _, err := c.ListDocCommentReplies(ctx, creds, "doxTok", "docx", "c1", "p2"); err != nil {
		t.Fatalf("ListDocCommentReplies: %v", err)
	}
	if meta, err := c.GetDocMeta(ctx, creds, "doxTok", "docx"); err != nil || meta.Title != "Spec" || meta.URL == "" {
		t.Fatalf("GetDocMeta = %+v, %v", meta, err)
	}
	if id, err := c.CreateDocCommentReply(ctx, creds, "doxTok", "docx", "c1", []DocCommentElement{textEl("ok")}); err != nil || id != "r9" {
		t.Fatalf("CreateDocCommentReply = %q, %v", id, err)
	}
	if id, err := c.CreateDocComment(ctx, creds, "doxTok", "docx", []DocCommentElement{textEl("ok")}); err != nil || id != "c9" {
		t.Fatalf("CreateDocComment = %q, %v", id, err)
	}
	if err := c.AddDocCommentReaction(ctx, creds, "doxTok", "docx", "r1", "Typing"); err != nil {
		t.Fatalf("AddDocCommentReaction: %v", err)
	}
	if err := c.RemoveDocCommentReaction(ctx, creds, "doxTok", "docx", "r1", "Typing"); err != nil {
		t.Fatalf("RemoveDocCommentReaction: %v", err)
	}

	want := []struct{ method, path, query, body string }{
		{"POST", "/open-apis/drive/v1/files/doxTok/comments/batch_query", "file_type=docx&user_id_type=open_id", `{"comment_ids":["c1"]}`},
		{"GET", "/open-apis/drive/v1/files/doxTok/comments/c1/replies", "file_type=docx&page_size=100&page_token=p2&user_id_type=open_id", `null`},
		{"POST", "/open-apis/drive/v1/metas/batch_query", "", `{"request_docs":[{"doc_token":"doxTok","doc_type":"docx"}],"with_url":true}`},
		{"POST", "/open-apis/drive/v1/files/doxTok/comments/c1/replies", "file_type=docx&user_id_type=open_id", `{"content":{"elements":[{"text_run":{"text":"ok"},"type":"text_run"}]}}`},
		{"POST", "/open-apis/drive/v1/files/doxTok/comments", "file_type=docx&user_id_type=open_id", `{"reply_list":{"replies":[{"content":{"elements":[{"text_run":{"text":"ok"},"type":"text_run"}]}}]}}`},
		{"POST", "/open-apis/drive/v2/files/doxTok/comments/reaction", "file_type=docx", `{"action":"add","reaction_type":"Typing","reply_id":"r1"}`},
		{"POST", "/open-apis/drive/v2/files/doxTok/comments/reaction", "file_type=docx", `{"action":"delete","reaction_type":"Typing","reply_id":"r1"}`},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, w := range want {
		body, _ := json.Marshal(calls[i].body)
		if calls[i].method != w.method || calls[i].path != w.path || calls[i].query != w.query || string(body) != w.body {
			t.Errorf("call %d = %s %s?%s %s\nwant      %s %s?%s %s", i, calls[i].method, calls[i].path, calls[i].query, body, w.method, w.path, w.query, w.body)
		}
	}
}

func TestHTTPClientDocCommentErrorCarriesCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("t-doc", 7200)
	fake.mux.HandleFunc("/open-apis/drive/v1/files/doxTok/comments/c1/replies", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"code": codeDocCommentParamError, "msg": "param error"})
	})
	_, err := newTestClient(fake, time.Now).CreateDocCommentReply(context.Background(), testCreds(), "doxTok", "docx", "c1", []DocCommentElement{textEl("ok")})
	if larkErrorCode(err) != codeDocCommentParamError {
		t.Fatalf("error must carry Feishu's code for the fallback: %v", err)
	}
}
