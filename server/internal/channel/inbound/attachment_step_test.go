package inbound_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/gateway"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// Test doubles for attachment step
// ---------------------------------------------------------------------------

type fakeAttachmentUploader struct {
	uploadKey   string
	uploadURL   string
	uploadErr   error
	uploadCalls []struct {
		Key         string
		Data        []byte
		ContentType string
		Filename    string
	}
}

func (f *fakeAttachmentUploader) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	f.uploadCalls = append(f.uploadCalls, struct {
		Key         string
		Data        []byte
		ContentType string
		Filename    string
	}{key, data, contentType, filename})
	return f.uploadURL, f.uploadErr
}
func (f *fakeAttachmentUploader) Delete(ctx context.Context, key string)        {}
func (f *fakeAttachmentUploader) DeleteKeys(ctx context.Context, keys []string) {}
func (f *fakeAttachmentUploader) KeyFromURL(rawURL string) string               { return "" }
func (f *fakeAttachmentUploader) CdnDomain() string                             { return "" }
func (f *fakeAttachmentUploader) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeAttachmentQuerier struct {
	created      []facade.UploadIssueAttachmentReq
	createReturn facade.Attachment
	createErr    error
}

func (f *fakeAttachmentQuerier) UploadIssueAttachment(ctx context.Context, req facade.UploadIssueAttachmentReq) (facade.Attachment, error) {
	f.created = append(f.created, req)
	return f.createReturn, f.createErr
}

type fakeFileDownloader struct {
	imageData          []byte
	imageErr           error
	fileData           []byte
	fileName           string
	fileErr            error
	downloadImageCalls []struct{ MessageID, FileKey string }
	downloadFileCalls  []struct{ MessageID, FileKey string }
}

func (f *fakeFileDownloader) DownloadImage(ctx context.Context, messageID, fileKey string) ([]byte, error) {
	f.downloadImageCalls = append(f.downloadImageCalls, struct{ MessageID, FileKey string }{messageID, fileKey})
	return f.imageData, f.imageErr
}

func (f *fakeFileDownloader) DownloadFile(ctx context.Context, messageID, fileKey string) ([]byte, string, error) {
	f.downloadFileCalls = append(f.downloadFileCalls, struct{ MessageID, FileKey string }{messageID, fileKey})
	return f.fileData, f.fileName, f.fileErr
}

func buildAttachmentStepConfig() (inbound.AttachmentConfig, *fakeAttachmentUploader, *fakeAttachmentQuerier, *fakeFileDownloader, *recordingChannel) {
	uploader := &fakeAttachmentUploader{uploadURL: "https://cdn.example.com/attachments/ws/iss/file.png"}
	querier := &fakeAttachmentQuerier{createReturn: facade.Attachment{ID: pgtype.UUID{Valid: true}}}
	downloader := &fakeFileDownloader{imageData: []byte("fake-image")}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           uploader,
		AttachmentQuerier: querier,
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}, DisplayName: "测试用户"}},
		IssueFacade:       facade.NewIssueFacade(&fakeIssueService{}),
	}
	return cfg, uploader, querier, downloader, recCh
}

func makeAttachmentEvt(attachments []port.AttachmentInfo, text string) port.InboundEvent {
	return port.InboundEvent{
		ChannelName: "feishu",
		EventID:     "evt-att-1",
		ChatID:      "chat-1",
		SenderID:    "ou_sender1",
		Type:        port.EventTypeMessageReceived,
		Text:        text,
		Attachments: attachments,
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-1: no attachments → bypass with DecisionContinue
// ---------------------------------------------------------------------------

func TestAttachmentStep_NoAttachments_Bypass(t *testing.T) {
	t.Parallel()

	cfg, uploader, querier, downloader, recCh := buildAttachmentStepConfig()
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt(nil, "hello")
	out, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(uploader.uploadCalls) != 0 {
		t.Error("Upload should not be called")
	}
	if len(querier.created) != 0 {
		t.Error("CreateAttachment should not be called")
	}
	if len(downloader.downloadImageCalls) != 0 {
		t.Error("DownloadImage should not be called")
	}
	if len(recCh.sends) != 0 {
		t.Error("no reply should be sent")
	}
	if out.EventID != evt.EventID {
		t.Error("event should be returned unchanged")
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-2: attachment present but no issue identifier → prompt user
// ---------------------------------------------------------------------------

func TestAttachmentStep_NoIdentifier_PromptsUser(t *testing.T) {
	t.Parallel()

	cfg, uploader, querier, _, recCh := buildAttachmentStepConfig()
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_abc", FileType: "image"},
	}, "just an image without issue key")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(uploader.uploadCalls) != 0 {
		t.Error("Upload should not be called")
	}
	if len(querier.created) != 0 {
		t.Error("CreateAttachment should not be called")
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "Issue 编号") {
		t.Errorf("reply should prompt for issue key: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-3: happy path — image attachment with issue identifier
// ---------------------------------------------------------------------------

func TestAttachmentStep_ImageWithIdentifier_HappyPath(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:          pgtype.UUID{Bytes: [16]byte{0x30}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true},
		Identifier:  "STA-68",
		Title:       "Test Issue",
		Status:      "todo",
	}

	uploader := &fakeAttachmentUploader{uploadURL: "https://cdn.example.com/att.png"}
	querier := &fakeAttachmentQuerier{createReturn: facade.Attachment{ID: pgtype.UUID{Valid: true}}}
	downloader := &fakeFileDownloader{imageData: []byte("fake-image-bytes")}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           uploader,
		AttachmentQuerier: querier,
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_abc", FileType: "image", FileName: "screenshot.png"},
	}, "upload to STA-68")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}

	// Download
	if len(downloader.downloadImageCalls) != 1 {
		t.Fatalf("expected 1 DownloadImage, got %d", len(downloader.downloadImageCalls))
	}
	if downloader.downloadImageCalls[0].FileKey != "img_abc" {
		t.Errorf("fileKey = %q, want img_abc", downloader.downloadImageCalls[0].FileKey)
	}

	// Upload
	if len(uploader.uploadCalls) != 1 {
		t.Fatalf("expected 1 Upload, got %d", len(uploader.uploadCalls))
	}
	up := uploader.uploadCalls[0]
	if !strings.Contains(up.Key, "attachments/") {
		t.Errorf("key = %q, should contain 'attachments/'", up.Key)
	}
	if string(up.Data) != "fake-image-bytes" {
		t.Errorf("data mismatch")
	}

	// DB
	if len(querier.created) != 1 {
		t.Fatalf("expected 1 CreateAttachment, got %d", len(querier.created))
	}
	att := querier.created[0]
	wsID := pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}
	issueID := pgtype.UUID{Bytes: [16]byte{0x30}, Valid: true}
	uploaderID := pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}
	if att.WorkspaceID != wsID {
		t.Error("workspace ID mismatch")
	}
	if att.IssueID != issueID {
		t.Error("issue ID mismatch")
	}
	if att.UploaderID != uploaderID {
		t.Error("uploader ID mismatch")
	}
	if att.UploaderType != "member" {
		t.Errorf("uploader_type = %q, want member", att.UploaderType)
	}
	if att.Filename != "screenshot.png" {
		t.Errorf("filename = %q, want screenshot.png", att.Filename)
	}
	if att.URL != "https://cdn.example.com/att.png" {
		t.Errorf("url = %q", att.URL)
	}

	// Reply
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "ATTACHMENT_OK") {
		t.Errorf("reply missing ATTACHMENT_OK: %q", recCh.sends[0].Text)
	}
	if !strings.Contains(recCh.sends[0].Text, "screenshot.png") {
		t.Errorf("reply should contain filename: %q", recCh.sends[0].Text)
	}
	if !strings.Contains(recCh.sends[0].Text, "STA-68") {
		t.Errorf("reply should contain issue key: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-4: file attachment happy path
// ---------------------------------------------------------------------------

func TestAttachmentStep_FileWithIdentifier_HappyPath(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:          pgtype.UUID{Bytes: [16]byte{0x31}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true},
		Identifier:  "STA-69",
		Title:       "File Test",
		Status:      "todo",
	}

	uploader := &fakeAttachmentUploader{uploadURL: "https://cdn.example.com/doc.pdf"}
	querier := &fakeAttachmentQuerier{createReturn: facade.Attachment{ID: pgtype.UUID{Valid: true}}}
	downloader := &fakeFileDownloader{fileData: []byte("fake-pdf"), fileName: "report.pdf"}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           uploader,
		AttachmentQuerier: querier,
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "file_xyz", FileType: "file", FileName: "report.pdf"},
	}, "attach to STA-69")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}

	if len(downloader.downloadFileCalls) != 1 {
		t.Fatalf("expected 1 DownloadFile, got %d", len(downloader.downloadFileCalls))
	}
	if downloader.downloadFileCalls[0].FileKey != "file_xyz" {
		t.Errorf("fileKey = %q", downloader.downloadFileCalls[0].FileKey)
	}

	if len(querier.created) != 1 {
		t.Fatalf("expected 1 CreateAttachment, got %d", len(querier.created))
	}
	if querier.created[0].Filename != "report.pdf" {
		t.Errorf("filename = %q", querier.created[0].Filename)
	}
	if !strings.Contains(recCh.sends[0].Text, "ATTACHMENT_OK") {
		t.Errorf("reply missing ATTACHMENT_OK: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-5: download failure → error reply
// ---------------------------------------------------------------------------

func TestAttachmentStep_DownloadFailure_ErrorReply(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         pgtype.UUID{Bytes: [16]byte{0x30}, Valid: true},
		Identifier: "STA-68",
	}

	downloader := &fakeFileDownloader{imageErr: errors.New("download failed")}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           &fakeAttachmentUploader{},
		AttachmentQuerier: &fakeAttachmentQuerier{},
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_abc", FileType: "image"},
	}, "STA-68")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "ATTACHMENT_FAIL") {
		t.Errorf("reply missing ATTACHMENT_FAIL: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-6: upload failure → error reply
// ---------------------------------------------------------------------------

func TestAttachmentStep_UploadFailure_ErrorReply(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         pgtype.UUID{Bytes: [16]byte{0x30}, Valid: true},
		Identifier: "STA-68",
	}

	uploader := &fakeAttachmentUploader{uploadErr: errors.New("s3 down")}
	downloader := &fakeFileDownloader{imageData: []byte("fake-image")}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           uploader,
		AttachmentQuerier: &fakeAttachmentQuerier{},
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_abc", FileType: "image"},
	}, "STA-68")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "ATTACHMENT_FAIL") {
		t.Errorf("reply missing ATTACHMENT_FAIL: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-7: issue not found → error reply
// ---------------------------------------------------------------------------

func TestAttachmentStep_IssueNotFound_ErrorReply(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierErr = errors.New("not found")

	downloader := &fakeFileDownloader{imageData: []byte("fake-image")}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           &fakeAttachmentUploader{},
		AttachmentQuerier: &fakeAttachmentQuerier{},
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_abc", FileType: "image"},
	}, "STA-999")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
	if !strings.Contains(recCh.sends[0].Text, "ISSUE_NOT_FOUND") {
		t.Errorf("reply missing ISSUE_NOT_FOUND: %q", recCh.sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-8: multiple attachments → process all
// ---------------------------------------------------------------------------

func TestAttachmentStep_MultipleAttachments_Processed(t *testing.T) {
	t.Parallel()

	issueSvc := &fakeIssueService{}
	issueSvc.getByIdentifierRet = facade.Issue{
		ID:         pgtype.UUID{Bytes: [16]byte{0x30}, Valid: true},
		Identifier: "STA-68",
	}

	uploader := &fakeAttachmentUploader{uploadURL: "https://cdn.example.com/att"}
	querier := &fakeAttachmentQuerier{createReturn: facade.Attachment{ID: pgtype.UUID{Valid: true}}}
	downloader := &fakeFileDownloader{imageData: []byte("img1"), fileData: []byte("pdf1"), fileName: "doc.pdf"}
	recCh := &recordingChannel{name: "feishu"}
	reg := channel.NewRegistry()
	_ = reg.Register(recCh)
	gw := gateway.NewRegistryGateway(reg)

	cfg := inbound.AttachmentConfig{
		Storage:           uploader,
		AttachmentQuerier: querier,
		FileDownloader:    downloader,
		Gateway:           gw,
		ReplySink:         inbound.NewGatewayReplySink(gw),
		ChatBinding:       &fakeChatBinding{wsID: pgtype.UUID{Bytes: [16]byte{0x01}, Valid: true}},
		UserResolver:      &fakeUserResolver{user: inbound.ResolvedUser{MulticaUserID: pgtype.UUID{Bytes: [16]byte{0x02}, Valid: true}}},
		IssueFacade:       facade.NewIssueFacade(issueSvc),
	}
	step := inbound.NewAttachmentStep(cfg)

	evt := makeAttachmentEvt([]port.AttachmentInfo{
		{FileKey: "img_1", FileType: "image", FileName: "a.png"},
		{FileKey: "file_1", FileType: "file", FileName: "doc.pdf"},
	}, "STA-68")
	_, d, err := step.Run(context.Background(), evt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if d != inbound.DecisionContinue {
		t.Errorf("decision = %v, want Continue", d)
	}
	if len(querier.created) != 2 {
		t.Fatalf("expected 2 DB records, got %d", len(querier.created))
	}
	if len(recCh.sends) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(recCh.sends))
	}
}

// ---------------------------------------------------------------------------
// TC-att-step-9: extractIssueIdentifier helper
// ---------------------------------------------------------------------------

func TestExtractIssueIdentifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"STA-68", "STA-68"},
		{"upload to STA-68", "STA-68"},
		{"请看 [MUL-123] 的截图", "MUL-123"},
		{"no issue key here", ""},
		{"", ""},
		{"abc-def not valid", ""},
	}

	for _, tc := range cases {
		got := inbound.ExtractIssueIdentifier(tc.input)
		if got != tc.want {
			t.Errorf("ExtractIssueIdentifier(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Name()
// ---------------------------------------------------------------------------

func TestAttachmentStep_Name(t *testing.T) {
	t.Parallel()

	cfg, _, _, _, _ := buildAttachmentStepConfig()
	step := inbound.NewAttachmentStep(cfg)
	if got := step.Name(); got != "attachment" {
		t.Errorf("Name = %q, want %q", got, "attachment")
	}
}
