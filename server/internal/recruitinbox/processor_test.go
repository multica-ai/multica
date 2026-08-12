package recruitinbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type memoryLedger struct{ records map[string]Record }

func newMemoryLedger() *memoryLedger { return &memoryLedger{records: map[string]Record{}} }
func (m *memoryLedger) Claim(_ context.Context, key, source string, at time.Time) (bool, error) {
	if _, ok := m.records[key]; ok {
		return false, nil
	}
	m.records[key] = Record{MessageKey: key, SourceMessageID: source, State: StateProcessing, ReceivedAt: at, UpdatedAt: at}
	return true, nil
}
func (m *memoryLedger) Pending(context.Context) ([]Record, error) { return nil, nil }
func (m *memoryLedger) MarkReplied(_ context.Context, key string, summary Summary, roleVersion, sent string, at time.Time) error {
	r := m.records[key]
	r.SourceMessageID, r.Summary, r.RoleVersion, r.State, r.SentMessageKey, r.SentStatus, r.UpdatedAt = "", summary, roleVersion, StateReplied, sent, "sent", at
	m.records[key] = r
	return nil
}
func (m *memoryLedger) MarkIgnored(context.Context, string, time.Time) error { return nil }
func (m *memoryLedger) MarkDeadLetter(_ context.Context, key, code, sent string, at time.Time) error {
	r := m.records[key]
	r.SourceMessageID, r.State, r.ErrorCode, r.SentMessageKey, r.UpdatedAt = "", StateDeadLetter, code, sent, at
	m.records[key] = r
	return nil
}
func (m *memoryLedger) Health(context.Context) error { return nil }
func (m *memoryLedger) Close() error                 { return nil }

type fakeAnalyzer struct {
	textCalls  int
	imageCalls int
	audioCalls int
	fileCalls  int
	lastText   string
	out        Extraction
}

func (a *fakeAnalyzer) AnalyzeText(_ context.Context, text string) (Extraction, error) {
	a.textCalls++
	a.lastText = text
	return a.out, nil
}
func (a *fakeAnalyzer) AnalyzeImage(context.Context, []byte, string) (Extraction, error) {
	a.imageCalls++
	return a.out, nil
}
func (a *fakeAnalyzer) AnalyzeFile(context.Context, []byte, string, string) (Extraction, error) {
	a.fileCalls++
	return a.out, nil
}
func (a *fakeAnalyzer) Transcribe(context.Context, io.Reader, string) (string, error) {
	a.audioCalls++
	return "transcribed requirement", nil
}

type trackedBody struct {
	*bytes.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type fakeFeishu struct {
	replies     []string
	replyKeys   []string
	downloads   int
	body        *trackedBody
	replyErr    error
	filename    string
	contentType string
}

func (f *fakeFeishu) Get(context.Context, string) (Inbound, error) { return Inbound{}, nil }
func (f *fakeFeishu) Download(context.Context, string, ResourceRef) (Resource, error) {
	f.downloads++
	if f.body == nil {
		f.body = &trackedBody{Reader: bytes.NewReader([]byte("media"))}
	}
	filename := f.filename
	if filename == "" {
		filename = "voice.ogg"
	}
	contentType := f.contentType
	if contentType == "" {
		contentType = "image/png"
	}
	return Resource{Body: f.body, Filename: filename, ContentType: contentType}, nil
}
func (f *fakeFeishu) Reply(_ context.Context, _ string, text, key string) (string, error) {
	f.replies = append(f.replies, text)
	f.replyKeys = append(f.replyKeys, key)
	if f.replyErr != nil {
		return "", f.replyErr
	}
	return "sent-message", nil
}

func testProcessor(t *testing.T, analyzer *fakeAnalyzer, feishu *fakeFeishu) (*Processor, *memoryLedger) {
	t.Helper()
	ledger := newMemoryLedger()
	p, err := NewProcessor(Config{AllowedChatID: "allowed", BotOpenID: "bot", HashKey: bytes.Repeat([]byte{7}, 32), RoleVersion: "role-v3", Now: func() time.Time { return time.Unix(100, 0) }}, ledger, analyzer, feishu)
	if err != nil {
		t.Fatal(err)
	}
	return p, ledger
}

func textInbound(id string) Inbound {
	return Inbound{MessageID: id, ChatID: "allowed", SenderID: "human", SenderType: "user", MessageType: "text", Content: `{"text":"new role"}`}
}

func TestDuplicateDeliveryProducesOneReply(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{}
	p, ledger := testProcessor(t, a, f)
	if err := p.Handle(context.Background(), textInbound("m1")); err != nil {
		t.Fatal(err)
	}
	if err := p.Handle(context.Background(), textInbound("m1")); err != nil {
		t.Fatal(err)
	}
	if len(f.replies) != 1 || a.textCalls != 1 || len(ledger.records) != 1 {
		t.Fatalf("replies=%d analysis=%d records=%d", len(f.replies), a.textCalls, len(ledger.records))
	}
	for _, r := range ledger.records {
		if r.State != StateReplied || r.SourceMessageID != "" || r.RoleVersion != "role-v3" {
			t.Fatalf("unexpected terminal record: %+v", r)
		}
		if r.MessageKey == "m1" || r.SentMessageKey == "sent-message" {
			t.Fatal("persistent identifiers must be HMAC values")
		}
	}
	if f.replyKeys[0] == "" || f.replyKeys[0] == "m1" {
		t.Fatal("reply must carry a stable opaque idempotency key")
	}
	if len(f.replyKeys[0]) > 50 {
		t.Fatalf("Feishu uuid exceeds 50-character limit: %d", len(f.replyKeys[0]))
	}
}

func TestOtherChatAndBotMessagesAreSilent(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{}
	p, ledger := testProcessor(t, a, f)
	other := textInbound("m-other")
	other.ChatID = "other"
	bot := textInbound("m-bot")
	bot.SenderType = "app"
	for _, in := range []Inbound{other, bot} {
		if err := p.Handle(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.replies) != 0 || a.textCalls != 0 || len(ledger.records) != 0 {
		t.Fatalf("silent filters had side effects: replies=%d analysis=%d records=%d", len(f.replies), a.textCalls, len(ledger.records))
	}
}

func TestImageAndAudioUseTransientMediaAndCloseIt(t *testing.T) {
	for _, tc := range []struct {
		name, kind, content  string
		wantImage, wantAudio int
	}{
		{name: "image", kind: "image", content: `{"image_key":"img-secret"}`, wantImage: 1},
		{name: "audio", kind: "audio", content: `{"file_key":"audio-secret"}`, wantAudio: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := &fakeAnalyzer{}, &fakeFeishu{}
			p, _ := testProcessor(t, a, f)
			in := textInbound("m-" + tc.name)
			in.MessageType, in.Content = tc.kind, tc.content
			if err := p.Handle(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			if a.imageCalls != tc.wantImage || a.audioCalls != tc.wantAudio || f.body == nil || !f.body.closed {
				t.Fatalf("image=%d audio=%d closed=%v", a.imageCalls, a.audioCalls, f.body != nil && f.body.closed)
			}
			if tc.wantAudio == 1 && (a.textCalls != 1 || a.lastText != "transcribed requirement") {
				t.Fatal("audio transcript was not analyzed")
			}
		})
	}
}

func TestConsequentialInstructionRequiresConfirmationAndExecutesNothing(t *testing.T) {
	a := &fakeAnalyzer{out: Extraction{Role: "工程师", RuleChange: true, Consequential: true, ProposedNextStep: "人工复核", Clarification: "适用哪个岗位"}}
	f := &fakeFeishu{}
	p, _ := testProcessor(t, a, f)
	if err := p.Handle(context.Background(), textInbound("hard-gate")); err != nil {
		t.Fatal(err)
	}
	got := f.replies[0]
	for _, want := range []string{"只记录和分析信息", "确认生效", "不会执行该变更", "需要确认：适用哪个岗位？"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reply missing %q: %s", want, got)
		}
	}
	if strings.Count(got, "需要确认：") != 1 {
		t.Fatal("automated reply may ask at most one clarification question")
	}
}

func TestDeterministicHardGateOverridesMissedModelClassification(t *testing.T) {
	a := &fakeAnalyzer{out: Extraction{Consequential: false}}
	f := &fakeFeishu{}
	p, _ := testProcessor(t, a, f)
	in := textInbound("hard-gate-fallback")
	in.Content = `{"text":"立即拒绝候选人并调整预算"}`
	if err := p.Handle(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.replies[0], "确认生效") || !strings.Contains(f.replies[0], "不会执行该变更") {
		t.Fatalf("hard-gate fallback missing: %s", f.replies[0])
	}
}

func TestPostContentIsFlattenedBeforeAnalysis(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{}
	p, _ := testProcessor(t, a, f)
	in := textInbound("post")
	in.MessageType = "post"
	in.Content = `{"zh_cn":{"title":"role","content":[[{"tag":"text","text":"backend engineer"}],[{"tag":"text","text":"start next month"}]]}}`
	if err := p.Handle(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.lastText, "backend engineer") || !strings.Contains(a.lastText, "start next month") {
		t.Fatalf("post content not flattened: %q", a.lastText)
	}
}

func TestUnsupportedFileGetsSafeDeadLetterReply(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{filename: "archive.zip", contentType: "application/zip"}
	p, ledger := testProcessor(t, a, f)
	in := textInbound("file")
	in.MessageType, in.Content = "file", `{"file_key":"private-key"}`
	if err := p.Handle(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(f.replies) != 1 || !strings.Contains(f.replies[0], "改发文字或受支持的导出格式") {
		t.Fatalf("unexpected failure reply: %v", f.replies)
	}
	for _, r := range ledger.records {
		if r.State != StateDeadLetter || r.ErrorCode != ErrorUnsupported || r.SourceMessageID != "" {
			t.Fatalf("unexpected record: %+v", r)
		}
	}
}

func TestSupportedFileIsAnalyzedTransiently(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{filename: "role.pdf", contentType: "application/pdf"}
	p, _ := testProcessor(t, a, f)
	in := textInbound("file-pdf")
	in.MessageType, in.Content = "file", `{"file_key":"private-key","file_name":"role.pdf"}`
	if err := p.Handle(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if a.fileCalls != 1 || f.body == nil || !f.body.closed || len(f.replies) != 1 {
		t.Fatalf("file_calls=%d closed=%v replies=%d", a.fileCalls, f.body != nil && f.body.closed, len(f.replies))
	}
}

func TestAmbiguousReplyFailureDoesNotSendSecondMessage(t *testing.T) {
	a, f := &fakeAnalyzer{}, &fakeFeishu{replyErr: errors.New("timeout")}
	p, ledger := testProcessor(t, a, f)
	if err := p.Handle(context.Background(), textInbound("ambiguous")); err != nil {
		t.Fatal(err)
	}
	if len(f.replies) != 1 {
		t.Fatalf("ambiguous send must not trigger a duplicate fallback reply: %d", len(f.replies))
	}
	for _, r := range ledger.records {
		if r.State != StateDeadLetter || r.ErrorCode != ErrorReply {
			t.Fatalf("unexpected record: %+v", r)
		}
	}
}

func TestPersistentSchemaHasNoPrivateContentColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "285_recruitment_inbox_event.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(raw))
	for _, forbidden := range []string{"chat_id", "raw_message", "message_content", "resource_key", "filename", "transcript", "resume", "contact", "salary", "evaluation"} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("persistent schema must not contain forbidden field %q", forbidden)
		}
	}
	for _, required := range []string{"message_key", "structured_summary", "role_version", "processing_state", "error_code", "sent_message_key", "sent_status"} {
		if !strings.Contains(schema, required) {
			t.Fatalf("persistent schema missing %q", required)
		}
	}
	indexRaw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "286_recruitment_inbox_event_message_key_index.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(string(indexRaw)), "CREATE UNIQUE INDEX CONCURRENTLY") {
		t.Fatal("message-key uniqueness must use a concurrent index migration")
	}
}
