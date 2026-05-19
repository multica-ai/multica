package inbound

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/storage"
)

const (
	replyAttachmentOK   = "ATTACHMENT_OK"
	replyAttachmentFail = "ATTACHMENT_FAIL"
)

// AttachmentQuerier is the persistence contract for creating attachment records.
type AttachmentQuerier interface {
	UploadIssueAttachment(ctx context.Context, req facade.UploadIssueAttachmentReq) (facade.Attachment, error)
}

// AttachmentConfig carries the dependencies the attachment step needs.
type AttachmentConfig struct {
	Storage           storage.Storage
	AttachmentQuerier AttachmentQuerier
	FileDownloader    port.FileDownloader
	Gateway           port.ChannelGateway
	ReplySink         ChannelReplySink
	ChatBinding       ChatBindingLookup
	UserResolver      UserInfoResolver
	IssueFacade       facade.IssueFacade
}

type attachmentStep struct {
	cfg AttachmentConfig
}

// NewAttachmentStep returns a Step that processes inbound message attachments.
// It downloads files from the external platform, uploads them to storage,
// creates attachment DB records, and replies to the chat with the result.
func NewAttachmentStep(cfg AttachmentConfig) Step {
	return &attachmentStep{cfg: cfg}
}

func (attachmentStep) Name() string { return "attachment" }

func (s *attachmentStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if len(evt.Attachments) == 0 {
		return evt, DecisionContinue, nil
	}

	issueKey := ExtractIssueIdentifier(evt.Text)
	if issueKey == "" {
		s.sendReply(ctx, evt, "请附上 Issue 编号（如 STA-68）以便上传附件。")
		return evt, DecisionContinue, nil
	}

	if !ValidIdentifierFormat(issueKey) {
		s.sendReply(ctx, evt, fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound))
		return evt, DecisionContinue, nil
	}

	wsID, err := s.cfg.ChatBinding.LookupWorkspaceID(ctx, evt.ConnectionID(), evt.ChatID)
	if err != nil {
		s.sendReply(ctx, evt, fmt.Sprintf("[%s] 处理请求时出错，请稍后重试。", replyInternalError))
		return evt, DecisionContinue, nil
	}

	issue, err := s.cfg.IssueFacade.GetIssueByIdentifier(ctx, wsID, issueKey)
	if err != nil {
		s.sendReply(ctx, evt, fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey))
		return evt, DecisionContinue, nil
	}

	user, err := s.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID)
	if err != nil {
		s.sendReply(ctx, evt, fmt.Sprintf("[%s] 处理请求时出错，请稍后重试。", replyInternalError))
		return evt, DecisionContinue, nil
	}

	var uploadedNames []string
	for _, att := range evt.Attachments {
		filename, err := s.processAttachment(ctx, evt, wsID, issue, user, att)
		if err != nil {
			slog.Error("attachment: process failed",
				"issue_key", issueKey,
				"file_key", att.FileKey,
				"error", err,
			)
			var msg string
			if strings.Contains(err.Error(), "download") {
				msg = fmt.Sprintf("[%s] 文件下载失败，请重试。", replyAttachmentFail)
			} else {
				msg = fmt.Sprintf("[%s] 文件上传失败，请稍后重试。", replyAttachmentFail)
			}
			s.sendReply(ctx, evt, msg)
			return evt, DecisionContinue, nil
		}
		uploadedNames = append(uploadedNames, filename)
	}

	reply := fmt.Sprintf("[%s] 已上传 %s 到 %s。", replyAttachmentOK, strings.Join(uploadedNames, ", "), issueKey)
	s.sendReply(ctx, evt, reply)
	return evt, DecisionContinue, nil
}

func (s *attachmentStep) processAttachment(ctx context.Context, evt port.InboundEvent, wsID pgtype.UUID, issue facade.Issue, user ResolvedUser, att port.AttachmentInfo) (string, error) {
	var data []byte
	var filename string
	var err error
	downloader, err := s.fileDownloader(evt)
	if err != nil {
		return "", err
	}

	switch att.FileType {
	case "image":
		data, err = downloader.DownloadImage(ctx, att.MessageID, att.FileKey)
		if err != nil {
			return "", fmt.Errorf("download image: %w", err)
		}
		filename = att.FileName
		if filename == "" {
			filename = att.FileKey + ".png"
		}
	case "file":
		data, filename, err = downloader.DownloadFile(ctx, att.MessageID, att.FileKey)
		if err != nil {
			return "", fmt.Errorf("download file: %w", err)
		}
		if filename == "" {
			filename = att.FileName
		}
		if filename == "" {
			filename = att.FileKey
		}
	default:
		return "", fmt.Errorf("unsupported file type: %s", att.FileType)
	}

	contentType := detectContentType(data, filename)
	key := fmt.Sprintf("attachments/%s/%s/%s-%s", wsIDToString(wsID), wsIDToString(issue.ID), uuid.Must(uuid.NewV7()).String(), filename)

	url, err := s.cfg.Storage.Upload(ctx, key, data, contentType, filename)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}

	_, err = s.cfg.AttachmentQuerier.UploadIssueAttachment(ctx, facade.UploadIssueAttachmentReq{
		WorkspaceID:  wsID,
		IssueID:      issue.ID,
		UploaderID:   user.MulticaUserID,
		UploaderType: "member",
		Filename:     filename,
		URL:          url,
		ContentType:  contentType,
		SizeBytes:    int64(len(data)),
	})
	if err != nil {
		return "", fmt.Errorf("create attachment: %w", err)
	}

	return filename, nil
}

func (s *attachmentStep) fileDownloader(evt port.InboundEvent) (port.FileDownloader, error) {
	if s.cfg.FileDownloader != nil {
		return s.cfg.FileDownloader, nil
	}
	if s.cfg.Gateway == nil {
		return nil, fmt.Errorf("attachment: channel gateway is not configured")
	}
	downloader, ok := s.cfg.Gateway.FileDownloader(evt.ConnectionID())
	if !ok || downloader == nil {
		return nil, fmt.Errorf("attachment: file downloader is not configured for connection %s", evt.ConnectionID())
	}
	return downloader, nil
}

func (s *attachmentStep) sendReply(ctx context.Context, evt port.InboundEvent, text string) {
	if s.cfg.ReplySink == nil {
		return
	}
	if err := s.cfg.ReplySink.SendText(ctx, evt, port.OutboundMessage{Target: port.TargetChat(evt.ChatID), Text: text}); err != nil {
		slog.Error("attachment: send reply failed", "channel", evt.ChannelName, "chat_id", evt.ChatID, "error", err)
	}
}

// issueKeyRe matches issue identifiers embedded in free-form text.
var issueKeyRe = regexp.MustCompile(`[A-Z]{2,5}-[1-9][0-9]*`)

// ExtractIssueIdentifier scans text for the first valid issue identifier.
// ExtractIssueIdentifier scans text for the first valid issue identifier.
// It is used by the attachment step (a non-intent path) to determine which
// issue an uploaded image/file should be attached to.
// Exported for testing.
func ExtractIssueIdentifier(text string) string {
	match := issueKeyRe.FindString(text)
	if match == "" {
		return ""
	}
	if ValidIdentifierFormat(match) {
		return match
	}
	return ""
}

// detectContentType sniffs the content type from file bytes and falls back to
// extension-based detection.
func detectContentType(data []byte, filename string) string {
	ct := http.DetectContentType(data)
	// Override with extension-based type for common mis-detections.
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".svg":
		ct = "image/svg+xml"
	case ".css":
		ct = "text/css"
	case ".js", ".mjs":
		ct = "application/javascript"
	case ".json":
		ct = "application/json"
	case ".pdf":
		ct = "application/pdf"
	case ".png":
		ct = "image/png"
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".gif":
		ct = "image/gif"
	case ".webp":
		ct = "image/webp"
	}
	return ct
}

// wsIDToString converts a pgtype.UUID to its string representation.
func wsIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
