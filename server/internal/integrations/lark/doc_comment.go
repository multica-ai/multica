package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// Cloud-document comments are a second Feishu conversation surface next to
// IM. When someone @-mentions the bot in a comment on a doc, sheet, slides,
// bitable or file, Feishu pushes drive.notice.comment_add_v1 over the same
// long connection as IM events. The adapter turns it into a group-chat-shaped
// InboundMessage so the shared pipeline (dedup, identity, @bot filter,
// session, run) applies unchanged, and the outbound side answers in the
// comment thread instead of an IM chat.
//
// Session scope: each anchored (local) comment thread is its own session; all
// whole-document comments on one file share a session, because Feishu cannot
// reply inside a whole-document comment — the answer is a new comment on the
// same file, so the conversation continues across comments.

const eventTypeDocCommentAdd = "drive.notice.comment_add_v1"

// docCommentChatIDPrefix keeps the synthetic chat key of a comment session
// apart from IM chat ids (oc_…) in the shared binding table.
const docCommentChatIDPrefix = "doc_comment:"

// DocCommentRef identifies the comment reply an inbound message came from.
// FileToken/FileType are the underlying document (for a wiki page, Feishu
// reports the page's own document, not the wiki node).
type DocCommentRef struct {
	FileToken string `json:"file_token"`
	FileType  string `json:"file_type"`
	CommentID string `json:"comment_id"`
	ReplyID   string `json:"reply_id"`
	// Whole marks a whole-document comment. It is only known after the
	// comment is fetched; an unfetched comment is treated as anchored.
	Whole bool `json:"whole,omitempty"`

	// botOpenID is the receiving bot, carried from the decoder (which has
	// the installation) to the enricher (which does not) to tell a bot
	// mention from other mentions. Not persisted.
	botOpenID string
}

// docCommentRoute is what the outbound side needs to answer a comment
// session, persisted on the binding config. CommentID is empty for a
// whole-document session, whose answers are new whole-document comments.
type docCommentRoute struct {
	FileToken string `json:"file_token"`
	FileType  string `json:"file_type"`
	CommentID string `json:"comment_id,omitempty"`
}

func (r DocCommentRef) route() docCommentRoute {
	route := docCommentRoute{FileToken: r.FileToken, FileType: r.FileType}
	if !r.Whole {
		route.CommentID = r.CommentID
	}
	return route
}

func (r DocCommentRef) chatID() ChatID {
	if r.Whole {
		return ChatID(docCommentChatIDPrefix + r.FileToken)
	}
	return ChatID(docCommentChatIDPrefix + r.FileToken + ":" + r.CommentID)
}

// docCommentRouteFromConfig reads the comment route off a binding or delivery
// config; ok is false for IM bindings.
func docCommentRouteFromConfig(config []byte) (docCommentRoute, bool) {
	if len(config) == 0 {
		return docCommentRoute{}, false
	}
	var cfg larkBindingConfig
	if err := json.Unmarshal(config, &cfg); err != nil || cfg.DocComment == nil || cfg.DocComment.FileToken == "" || cfg.DocComment.FileType == "" {
		return docCommentRoute{}, false
	}
	return *cfg.DocComment, true
}

// larkDocCommentEvent is the drive.notice.comment_add_v1 payload. The
// documented shape keeps the file fields in notice_meta and is_mentioned at
// the top level; live payloads have been observed with them the other way
// round, so both positions are read.
type larkDocCommentEvent struct {
	NoticeMeta  larkDocCommentNoticeMeta `json:"notice_meta"`
	FileToken   string                   `json:"file_token"`
	FileType    string                   `json:"file_type"`
	CommentID   string                   `json:"comment_id"`
	ReplyID     string                   `json:"reply_id"`
	IsMentioned *bool                    `json:"is_mentioned"`
}

type larkDocCommentNoticeMeta struct {
	FileToken   string      `json:"file_token"`
	FileType    string      `json:"file_type"`
	FromUserID  larkUserIDs `json:"from_user_id"`
	ToUserID    larkUserIDs `json:"to_user_id"`
	IsMentioned *bool       `json:"is_mentioned"`
}

// UnmarshalJSON accepts notice_meta as an object or as a JSON-encoded
// string; both shapes have been delivered.
func (m *larkDocCommentNoticeMeta) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		data = []byte(encoded)
	}
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	type plain larkDocCommentNoticeMeta
	return json.Unmarshal(data, (*plain)(m))
}

type larkUserIDs struct {
	OpenID  string `json:"open_id"`
	UnionID string `json:"union_id"`
	UserID  string `json:"user_id"`
}

// decodeDocCommentEvent maps a comment notice onto a group-chat-shaped
// InboundMessage. MessageID is the reply id (a new comment carries its first
// reply's id too), which is what dedup and the outbound mention key on. The
// body is fetched later by the enricher; AddressedToBot starts from Feishu's
// mention flag and is re-derived from the fetched thread.
func decodeDocCommentEvent(env larkEventEnvelope, inst Installation) (InboundMessage, bool, error) {
	if env.Event == nil {
		return InboundMessage{}, false, errors.New("doc comment event with empty payload")
	}
	var evt larkDocCommentEvent
	if err := json.Unmarshal(env.Event, &evt); err != nil {
		return InboundMessage{}, false, fmt.Errorf("doc comment event: %w", err)
	}
	ref := DocCommentRef{
		FileToken: firstNonEmpty(evt.NoticeMeta.FileToken, evt.FileToken),
		FileType:  firstNonEmpty(evt.NoticeMeta.FileType, evt.FileType),
		CommentID: evt.CommentID,
		ReplyID:   evt.ReplyID,
		botOpenID: inst.BotOpenID,
	}
	sender := evt.NoticeMeta.FromUserID.OpenID
	if ref.FileToken == "" || ref.FileType == "" || ref.CommentID == "" || ref.ReplyID == "" || sender == "" {
		return InboundMessage{}, false, errors.New("doc comment event missing file, comment, reply or sender id")
	}
	// The bot's own replies keep its threads alive; never answer them.
	if inst.BotOpenID != "" && sender == inst.BotOpenID {
		return InboundMessage{}, false, nil
	}
	mentioned := evt.IsMentioned
	if mentioned == nil {
		mentioned = evt.NoticeMeta.IsMentioned
	}
	// A mention of another bot also reaches this one when it follows the
	// thread; only a notice addressed to this bot counts.
	to := evt.NoticeMeta.ToUserID.OpenID
	addressed := mentioned != nil && *mentioned && (to == "" || inst.BotOpenID == "" || to == inst.BotOpenID)
	return InboundMessage{
		EventType:      env.Header.EventType,
		EventID:        env.Header.EventID,
		AppID:          env.Header.AppID,
		ChatID:         ref.chatID(),
		ChatType:       ChatTypeGroup,
		MessageID:      ref.ReplyID,
		SenderOpenID:   OpenID(sender),
		MessageType:    "text",
		CreateTime:     env.Header.CreateTime,
		AddressedToBot: addressed,
		DocComment:     &ref,
	}, true, nil
}

// DocCommentElement is one rich-text element of a comment reply. Feishu
// returns every sub-object on every element (unused ones null), so readers
// must switch on Type.
type DocCommentElement struct {
	Type     string               `json:"type"`
	TextRun  *DocCommentTextRun   `json:"text_run,omitempty"`
	DocsLink *DocCommentDocsLink  `json:"docs_link,omitempty"`
	Person   *DocCommentPersonRef `json:"person,omitempty"`
}

type DocCommentTextRun struct {
	Text string `json:"text"`
}

type DocCommentDocsLink struct {
	URL string `json:"url"`
}

type DocCommentPersonRef struct {
	UserID string `json:"user_id"`
}

// DocCommentReply is one reply in a comment thread (the first reply is the
// comment's own text). UserID is an open_id: every read asks for open_id.
type DocCommentReply struct {
	ReplyID string `json:"reply_id"`
	UserID  string `json:"user_id"`
	Content struct {
		Elements []DocCommentElement `json:"elements"`
	} `json:"content"`
}

// DocComment is a comment with (possibly truncated) replies.
type DocComment struct {
	CommentID string `json:"comment_id"`
	IsWhole   bool   `json:"is_whole"`
	Quote     string `json:"quote"`
	HasMore   bool   `json:"has_more"`
	ReplyList struct {
		Replies []DocCommentReply `json:"replies"`
	} `json:"reply_list"`
}

// DocCommentReplyPage is one page of a thread's replies.
type DocCommentReplyPage struct {
	Items     []DocCommentReply `json:"items"`
	HasMore   bool              `json:"has_more"`
	PageToken string            `json:"page_token"`
}

// DocMeta is the display metadata of a document.
type DocMeta struct {
	Title string
	URL   string
}

// docCommentAPIClient is the comment transport, an optional capability of
// the APIClient (the HTTP client has it; stubs and fakes that do not keep
// compiling, and comment events then degrade instead of failing).
type docCommentAPIClient interface {
	GetDocComment(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID string) (DocComment, error)
	ListDocCommentReplies(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID, pageToken string) (DocCommentReplyPage, error)
	GetDocMeta(ctx context.Context, creds InstallationCredentials, fileToken, fileType string) (DocMeta, error)
	CreateDocCommentReply(ctx context.Context, creds InstallationCredentials, fileToken, fileType, commentID string, elements []DocCommentElement) (string, error)
	CreateDocComment(ctx context.Context, creds InstallationCredentials, fileToken, fileType string, elements []DocCommentElement) (string, error)
	AddDocCommentReaction(ctx context.Context, creds InstallationCredentials, fileToken, fileType, replyID, reactionType string) error
	RemoveDocCommentReaction(ctx context.Context, creds InstallationCredentials, fileToken, fileType, replyID, reactionType string) error
}

// docCommentReceivedReaction marks a comment the bot is working on, the
// comment counterpart of the IM typing indicator. It is removed when the run
// ends (answered, failed or cancelled).
const docCommentReceivedReaction = "Typing"

const (
	// docCommentMaxReplyPages bounds the fallback pagination when the
	// triggering reply is not in the first batch of a long thread.
	docCommentMaxReplyPages = 5
	// docCommentMaxContextReplies keeps a long thread from flooding the
	// prompt; the newest earlier replies are kept.
	docCommentMaxContextReplies = 30
	docCommentMaxQuoteRunes     = 2000
)

// enrichDocComment fetches the comment thread and renders it into the body:
// the document, the commented text, the earlier replies, then the trigger
// reply as the user's own text. It also settles whether the comment is a
// whole-document one (which changes the session key) and whether the reply
// is addressed to the bot: it @-mentions the bot, or it follows up in an
// anchored thread the bot has already answered in (a thread with the bot
// reads as a conversation, so repeating the mention on every reply is
// friction). Every fetch is best-effort within the enrichment budget.
func (e *inboundEnricher) enrichDocComment(ctx context.Context, msg InboundMessage, creds InstallationCredentials) InboundMessage {
	ref := *msg.DocComment
	botOpenID := ref.botOpenID
	api, ok := e.client.(docCommentAPIClient)
	if !ok || !e.client.IsConfigured() {
		msg.Body = renderDocCommentHeader(ref, DocMeta{}) + "\n</feishu_doc_comment>\n\n" + docCommentUnavailableLine
		return msg
	}

	var (
		replies    []DocCommentReply
		comment    DocComment
		commentErr error
		meta       DocMeta
		metaErr    error
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		comment, replies, commentErr = fetchDocCommentThread(ctx, api, creds, ref)
	}()
	go func() {
		defer wg.Done()
		meta, metaErr = api.GetDocMeta(ctx, creds, ref.FileToken, ref.FileType)
	}()
	wg.Wait()
	if metaErr != nil {
		e.logger.Info("lark enricher: doc meta unavailable", "file_type", ref.FileType, "err", metaErr)
	}

	trigger := -1
	if commentErr != nil {
		e.logger.Warn("lark enricher: doc comment fetch failed", "file_type", ref.FileType, "comment_id", ref.CommentID, "err", commentErr)
	} else {
		ref.Whole = comment.IsWhole
		for i := range replies {
			if replies[i].ReplyID == ref.ReplyID {
				trigger = i
				break
			}
		}
		if trigger < 0 {
			e.logger.Warn("lark enricher: doc comment trigger reply not found", "comment_id", ref.CommentID, "replies", len(replies))
		}
	}
	msg.DocComment = &ref
	msg.ChatID = ref.chatID()

	var earlier []DocCommentReply
	if trigger >= 0 {
		earlier = replies[:trigger]
		if len(earlier) > docCommentMaxContextReplies {
			earlier = earlier[len(earlier)-docCommentMaxContextReplies:]
		}
	}
	ids := []string{string(msg.SenderOpenID)}
	for _, r := range earlier {
		ids = append(ids, r.UserID)
	}
	if trigger >= 0 {
		ids = append(ids, docCommentPersonIDs(replies[trigger].Content.Elements)...)
	}
	for _, r := range earlier {
		ids = append(ids, docCommentPersonIDs(r.Content.Elements)...)
	}
	names := map[string]string{}
	for id, name := range e.resolveNames(ctx, creds, withoutID(ids, botOpenID)) {
		names[id] = name
	}
	if botOpenID != "" {
		names[botOpenID] = "Bot" // earlier replies keep "@Bot"; the trigger drops it
	}

	if trigger >= 0 {
		own := renderDocCommentElements(replies[trigger].Content.Elements, botOpenID, names)
		msg.AddressedToBot = docCommentMentions(replies[trigger].Content.Elements, botOpenID) ||
			(!ref.Whole && repliedBy(replies[:trigger], botOpenID))
		msg.Body, msg.CommandBody = own, own
		msg, _ = e.selectContext(msg)
	}

	var b strings.Builder
	b.WriteString(renderDocCommentHeader(ref, meta))
	labeler := newSpeakerLabeler(names)
	if comment.Quote != "" && !ref.Whole {
		fmt.Fprintf(&b, "\n<quoted_text>\n%s\n</quoted_text>", truncateRunes(comment.Quote, docCommentMaxQuoteRunes))
	}
	if len(earlier) > 0 {
		fmt.Fprintf(&b, "\n<comment_thread count=\"%d\">", len(earlier))
		for _, r := range earlier {
			text := renderDocCommentElements(r.Content.Elements, "", names)
			if text == "" {
				text = "[empty reply]"
			}
			fmt.Fprintf(&b, "\n[%s]: %s", labeler.label(docCommentSpeaker(r.UserID, botOpenID)), text)
		}
		b.WriteString("\n</comment_thread>")
	}
	b.WriteString("\n</feishu_doc_comment>")

	switch {
	case trigger < 0:
		msg.Body = b.String() + "\n\n" + docCommentUnavailableLine
	case msg.Body == "":
		// A bare control command (/clear) keeps an empty body so the core
		// treats it as such; the context would only be noise.
	default:
		msg.Body = b.String() + "\n\n" + fmt.Sprintf("[%s]: %s", labeler.label(docCommentSpeaker(string(msg.SenderOpenID), botOpenID)), msg.Body)
	}
	return msg
}

const docCommentUnavailableLine = "[The comment text could not be fetched from Feishu. Read the comment from the document if you have access, or ask the user to repeat the request.]"

// fetchDocCommentThread returns the comment and its replies. Batch query
// returns the first replies only; when the trigger is not among them the
// thread is paged from the start.
func fetchDocCommentThread(ctx context.Context, api docCommentAPIClient, creds InstallationCredentials, ref DocCommentRef) (DocComment, []DocCommentReply, error) {
	comment, err := api.GetDocComment(ctx, creds, ref.FileToken, ref.FileType, ref.CommentID)
	if err != nil {
		return DocComment{}, nil, err
	}
	replies := comment.ReplyList.Replies
	if !comment.HasMore || containsReply(replies, ref.ReplyID) {
		return comment, replies, nil
	}
	var all []DocCommentReply
	pageToken := ""
	for page := 0; page < docCommentMaxReplyPages; page++ {
		p, err := api.ListDocCommentReplies(ctx, creds, ref.FileToken, ref.FileType, ref.CommentID, pageToken)
		if err != nil {
			return comment, replies, nil
		}
		all = append(all, p.Items...)
		if !p.HasMore || p.PageToken == "" || containsReply(p.Items, ref.ReplyID) {
			break
		}
		pageToken = p.PageToken
	}
	return comment, all, nil
}

func containsReply(replies []DocCommentReply, replyID string) bool {
	for _, r := range replies {
		if r.ReplyID == replyID {
			return true
		}
	}
	return false
}

func renderDocCommentHeader(ref DocCommentRef, meta DocMeta) string {
	kind := "anchored"
	if ref.Whole {
		kind = "whole_document"
	}
	attrs := fmt.Sprintf("file_type=%q file_token=%q comment_id=%q comment_kind=%q", ref.FileType, ref.FileToken, ref.CommentID, kind)
	if meta.Title != "" {
		attrs += fmt.Sprintf(" title=%q", meta.Title)
	}
	if meta.URL != "" {
		attrs += fmt.Sprintf(" url=%q", meta.URL)
	}
	return "<feishu_doc_comment " + attrs + ">"
}

// renderDocCommentElements flattens a reply to text, keeping its line
// breaks (a /issue description starts on the second line). Mentions of
// botOpenID are dropped with one adjoining space (as in IM, the bot mention
// is addressing, not content); other people render as @name.
func renderDocCommentElements(elements []DocCommentElement, botOpenID string, names map[string]string) string {
	var b strings.Builder
	afterBotMention := false
	for _, el := range elements {
		dropped := false
		switch el.Type {
		case "text_run":
			if el.TextRun != nil {
				text := el.TextRun.Text
				if afterBotMention {
					text = strings.TrimPrefix(text, " ")
				}
				b.WriteString(text)
			}
		case "docs_link":
			if el.DocsLink != nil {
				b.WriteString(el.DocsLink.URL)
			}
		case "person":
			if el.Person == nil || el.Person.UserID == "" {
				continue
			}
			if botOpenID != "" && el.Person.UserID == botOpenID {
				dropped = true
				break
			}
			name := names[el.Person.UserID]
			if name == "" {
				name = "user"
			}
			b.WriteString("@" + name)
		}
		afterBotMention = dropped
	}
	return strings.TrimSpace(b.String())
}

func docCommentMentions(elements []DocCommentElement, botOpenID string) bool {
	if botOpenID == "" {
		return false
	}
	for _, el := range elements {
		if el.Type == "person" && el.Person != nil && el.Person.UserID == botOpenID {
			return true
		}
	}
	return false
}

func repliedBy(replies []DocCommentReply, openID string) bool {
	if openID == "" {
		return false
	}
	for _, r := range replies {
		if r.UserID == openID {
			return true
		}
	}
	return false
}

func docCommentPersonIDs(elements []DocCommentElement) []string {
	var ids []string
	for _, el := range elements {
		if el.Type == "person" && el.Person != nil && el.Person.UserID != "" {
			ids = append(ids, el.Person.UserID)
		}
	}
	return ids
}

func withoutID(ids []string, drop string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if id != "" && id != drop {
			out = append(out, id)
		}
	}
	return out
}

// docCommentSpeaker adapts a reply author to the IM speaker labeler, which
// labels app senders "Bot".
func docCommentSpeaker(openID, botOpenID string) LarkMessage {
	if botOpenID != "" && openID == botOpenID {
		return LarkMessage{SenderID: openID, SenderType: "app"}
	}
	return LarkMessage{SenderID: openID, SenderType: "user"}
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// docCommentChunkRunes keeps each reply well under Feishu's 10,000-character
// cap on a reply's text.
const docCommentChunkRunes = 4000

// docCommentReplyChunks splits text into reply-sized element lists,
// preferring line breaks. The first chunk @-mentions mentionOpenID so a new
// whole-document comment still notifies the person who asked.
func docCommentReplyChunks(mentionOpenID, text string) [][]DocCommentElement {
	// Feishu HTML-unescapes comment text on write ("&lt;" is stored as "<"),
	// so each chunk is escaped (after splitting, so no entity is cut) for the
	// agent's text to arrive verbatim.
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	var chunks [][]DocCommentElement
	runes := []rune(text)
	for len(runes) > 0 {
		n := len(runes)
		if n > docCommentChunkRunes {
			n = docCommentChunkRunes
			for i := n - 1; i > docCommentChunkRunes/2; i-- {
				if runes[i] == '\n' {
					n = i + 1
					break
				}
			}
		}
		part := strings.TrimRight(string(runes[:n]), "\n")
		runes = runes[n:]
		if strings.TrimSpace(part) == "" {
			continue
		}
		var elems []DocCommentElement
		if len(chunks) == 0 && mentionOpenID != "" {
			elems = append(elems,
				DocCommentElement{Type: "person", Person: &DocCommentPersonRef{UserID: mentionOpenID}},
				DocCommentElement{Type: "text_run", TextRun: &DocCommentTextRun{Text: " "}})
		}
		elems = append(elems, DocCommentElement{Type: "text_run", TextRun: &DocCommentTextRun{Text: escape(part)}})
		chunks = append(chunks, elems)
	}
	return chunks
}

// codeDocCommentParamError is Feishu's generic comment "param error"; replying
// to a whole-document comment is one of its causes.
const codeDocCommentParamError = 1069302

// postDocCommentText answers a comment session: a reply in the anchored
// thread, or a new whole-document comment. A reply the thread rejects (a
// whole-document comment whose kind could not be fetched on the way in, or a
// resolved thread) falls back to a new comment where Feishu supports one.
func postDocCommentText(ctx context.Context, api docCommentAPIClient, creds InstallationCredentials, route docCommentRoute, mentionOpenID, text string) error {
	if api == nil {
		return errors.New("lark: doc comment transport not configured")
	}
	for _, elems := range docCommentReplyChunks(mentionOpenID, text) {
		if route.CommentID != "" {
			_, err := api.CreateDocCommentReply(ctx, creds, route.FileToken, route.FileType, route.CommentID, elems)
			if err == nil {
				continue
			}
			if larkErrorCode(err) != codeDocCommentParamError || !supportsWholeDocComment(route.FileType) {
				return fmt.Errorf("reply to doc comment: %w", err)
			}
			route.CommentID = ""
		}
		if _, err := api.CreateDocComment(ctx, creds, route.FileToken, route.FileType, elems); err != nil {
			return fmt.Errorf("create doc comment: %w", err)
		}
	}
	return nil
}

// supportsWholeDocComment reports whether Feishu's create-comment API accepts
// the file type (it documents doc and docx only).
func supportsWholeDocComment(fileType string) bool {
	return fileType == "doc" || fileType == "docx"
}

// AckDocComment marks an ingested comment trigger as being worked on. The
// patcher's end-of-task sweep removes it from the task's trigger reply (the
// delivery row is the anchor, so no IM cleanup registration). Best-effort.
func (m *TypingIndicatorManager) AckDocComment(ctx context.Context, inst Installation, ref DocCommentRef) {
	api, ok := m.client.(docCommentAPIClient)
	if !ok {
		return
	}
	creds, err := m.resolveCredentials(inst)
	if err != nil {
		m.log.Warn("lark: doc comment reaction credentials", "err", err)
		return
	}
	if err := api.AddDocCommentReaction(ctx, creds, ref.FileToken, ref.FileType, ref.ReplyID, docCommentReceivedReaction); err != nil {
		m.log.Info("lark: doc comment reaction failed", "comment_id", ref.CommentID, "err", err)
	}
}

// clearDocCommentReaction removes the working-on-it reaction from a comment
// trigger once its run has ended. Best-effort, like adding it.
func (p *Patcher) clearDocCommentReaction(ctx context.Context, creds InstallationCredentials, route docCommentRoute, replyID string) {
	if p.docComments == nil {
		return
	}
	if err := p.docComments.RemoveDocCommentReaction(ctx, creds, route.FileToken, route.FileType, replyID, docCommentReceivedReaction); err != nil {
		p.cfg.Logger.Info("lark: doc comment reaction removal failed", "reply_id", replyID, "err", err)
	}
}
