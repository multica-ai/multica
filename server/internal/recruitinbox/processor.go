package recruitinbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

const (
	ErrorAnalyze     = "analyze_failed"
	ErrorDownload    = "resource_download_failed"
	ErrorUnsupported = "unsupported_file"
	ErrorReply       = "reply_failed"
	ErrorInvalid     = "invalid_event"
	maxImageBytes    = 20 << 20
	maxAudioBytes    = 25 << 20
	maxFileBytes     = 20 << 20
	guardrail        = "本回复只记录和分析信息，不会自动生效规则、改变候选人状态或执行招聘动作。"
	failureNotice    = guardrail + "\n处理失败，请稍后重试；如仍失败，请改发文字或受支持的导出格式。"
)

type Config struct {
	AllowedChatID string
	BotOpenID     string
	HashKey       []byte
	RoleVersion   string
	Now           func() time.Time
	Logger        *slog.Logger
}

type Processor struct {
	cfg      Config
	ledger   Ledger
	analyzer Analyzer
	feishu   FeishuClient
}

func NewProcessor(cfg Config, ledger Ledger, analyzer Analyzer, feishu FeishuClient) (*Processor, error) {
	if strings.TrimSpace(cfg.AllowedChatID) == "" {
		return nil, errors.New("recruit inbox: allowed chat id is required")
	}
	if len(cfg.HashKey) < 32 {
		return nil, errors.New("recruit inbox: hash key must be at least 32 bytes")
	}
	if ledger == nil || analyzer == nil || feishu == nil {
		return nil, errors.New("recruit inbox: ledger, analyzer, and Feishu client are required")
	}
	if cfg.RoleVersion == "" {
		cfg.RoleVersion = "v1"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Processor{cfg: cfg, ledger: ledger, analyzer: analyzer, feishu: feishu}, nil
}

// Admit verifies the runtime allowlist and bot-loop guards, then durably claims
// the Feishu message ID before returning. A long-connection handler should ACK
// only after this succeeds, enqueueing Process separately so OCR/transcription
// never consumes Feishu's short ACK budget.
func (p *Processor) Admit(ctx context.Context, in Inbound) (bool, error) {
	if in.ChatID != p.cfg.AllowedChatID || isBotAuthored(in, p.cfg.BotOpenID) {
		return false, nil
	}
	if in.MessageID == "" {
		return false, nil
	}
	messageKey := p.hash("inbound", in.MessageID)
	claimed, err := p.ledger.Claim(ctx, messageKey, in.MessageID, nonZeroTime(in.CreatedAt, p.cfg.Now()))
	if err != nil {
		return false, fmt.Errorf("recruit inbox: claim ledger: %w", err)
	}
	return claimed, nil
}

// Process handles an already-claimed event. The source body, chat ID, resource
// key, transcript, and extracted values never enter logs or persistent storage.
func (p *Processor) Process(ctx context.Context, in Inbound) error {
	messageKey := p.hash("inbound", in.MessageID)
	extraction, code, err := p.extract(ctx, in)
	if err != nil {
		p.cfg.Logger.Warn("recruit inbox: extraction failed", "message_key", messageKey, "error_code", code)
		return p.deadLetter(ctx, in, messageKey, code)
	}
	reply := RenderReply(extraction)
	sentID, err := p.feishu.Reply(ctx, in.ChatID, reply, p.idempotencyKey("reply", in.MessageID))
	if err != nil {
		p.cfg.Logger.Warn("recruit inbox: reply failed", "message_key", messageKey, "error_code", ErrorReply)
		// Do not send a second failure notification after an ambiguous send error:
		// Feishu may have accepted the first reply. The stable uuid makes connector
		// redelivery safe, and the ledger keeps this event in dead-letter state.
		if markErr := p.ledger.MarkDeadLetter(ctx, messageKey, ErrorReply, "", p.cfg.Now()); markErr != nil {
			return fmt.Errorf("recruit inbox: reply and dead-letter failed: %w", markErr)
		}
		return nil
	}
	return p.ledger.MarkReplied(ctx, messageKey, summarize(extraction), p.cfg.RoleVersion, p.hash("sent", sentID), p.cfg.Now())
}

// Handle is the synchronous convenience used by tests and bounded callers.
func (p *Processor) Handle(ctx context.Context, in Inbound) error {
	claimed, err := p.Admit(ctx, in)
	if err != nil || !claimed {
		return err
	}
	return p.Process(ctx, in)
}

func (p *Processor) Pending(ctx context.Context) ([]Record, error) {
	return p.ledger.Pending(ctx)
}

func (p *Processor) extract(ctx context.Context, in Inbound) (Extraction, string, error) {
	switch in.MessageType {
	case "text", "post":
		text, err := textContent(in.Content)
		if err != nil || strings.TrimSpace(text) == "" {
			return Extraction{}, ErrorInvalid, errors.New("empty or invalid text content")
		}
		out, err := p.analyzer.AnalyzeText(ctx, text)
		applyDeterministicGuard(text, &out)
		return out, ErrorAnalyze, err
	case "image":
		ref, err := resourceRef(in.MessageType, in.Content)
		if err != nil {
			return Extraction{}, ErrorInvalid, err
		}
		resource, err := p.feishu.Download(ctx, in.MessageID, ref)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		defer resource.Body.Close()
		data, err := readBounded(resource.Body, maxImageBytes)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		out, err := p.analyzer.AnalyzeImage(ctx, data, resource.ContentType)
		return out, ErrorAnalyze, err
	case "audio", "media":
		ref, err := resourceRef(in.MessageType, in.Content)
		if err != nil {
			return Extraction{}, ErrorInvalid, err
		}
		resource, err := p.feishu.Download(ctx, in.MessageID, ref)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		defer resource.Body.Close()
		data, err := readBounded(resource.Body, maxAudioBytes)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		transcript, err := p.analyzer.Transcribe(ctx, bytes.NewReader(data), safeAudioFilename(resource.Filename, in.MessageType))
		if err != nil {
			return Extraction{}, ErrorAnalyze, err
		}
		out, err := p.analyzer.AnalyzeText(ctx, transcript)
		applyDeterministicGuard(transcript, &out)
		return out, ErrorAnalyze, err
	case "file":
		ref, err := resourceRef(in.MessageType, in.Content)
		if err != nil {
			return Extraction{}, ErrorInvalid, err
		}
		resource, err := p.feishu.Download(ctx, in.MessageID, ref)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		defer resource.Body.Close()
		filename := firstNonEmpty(resource.Filename, ref.Filename)
		if !supportedFile(filename, resource.ContentType) {
			return Extraction{}, ErrorUnsupported, errors.New("unsupported file")
		}
		data, err := readBounded(resource.Body, maxFileBytes)
		if err != nil {
			return Extraction{}, ErrorDownload, err
		}
		out, err := p.analyzer.AnalyzeFile(ctx, data, filename, resource.ContentType)
		return out, ErrorAnalyze, err
	default:
		return Extraction{}, ErrorUnsupported, errors.New("unsupported message type")
	}
}

func (p *Processor) deadLetter(ctx context.Context, in Inbound, messageKey, code string) error {
	sentID, sendErr := p.feishu.Reply(ctx, in.ChatID, failureNotice, p.idempotencyKey("dead-letter", in.MessageID))
	sentKey := ""
	if sendErr == nil {
		sentKey = p.hash("sent", sentID)
	}
	if err := p.ledger.MarkDeadLetter(ctx, messageKey, code, sentKey, p.cfg.Now()); err != nil {
		return fmt.Errorf("recruit inbox: mark dead letter: %w", err)
	}
	return nil
}

func (p *Processor) hash(namespace, value string) string {
	return hex.EncodeToString(p.hmac(namespace, value))
}

func (p *Processor) idempotencyKey(namespace, value string) string {
	return base64.RawURLEncoding.EncodeToString(p.hmac(namespace, value))
}

func (p *Processor) hmac(namespace, value string) []byte {
	mac := hmac.New(sha256.New, p.cfg.HashKey)
	_, _ = io.WriteString(mac, namespace)
	_, _ = io.WriteString(mac, "\x00")
	_, _ = io.WriteString(mac, value)
	return mac.Sum(nil)
}

func isBotAuthored(in Inbound, botOpenID string) bool {
	return in.SenderType == "app" || in.SenderType == "bot" ||
		(botOpenID != "" && in.SenderID == botOpenID)
}

func textContent(raw string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", err
	}
	var parts []string
	collectText(v, &parts)
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func collectText(value any, parts *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "text" {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					*parts = append(*parts, text)
					continue
				}
			}
			collectText(child, parts)
		}
	case []any:
		for _, child := range v {
			collectText(child, parts)
		}
	}
}

func resourceRef(messageType, raw string) (ResourceRef, error) {
	var v struct {
		ImageKey string `json:"image_key"`
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ResourceRef{}, err
	}
	if messageType == "image" && v.ImageKey != "" {
		return ResourceRef{Key: v.ImageKey, Kind: "image", Filename: "image"}, nil
	}
	if v.FileKey != "" {
		return ResourceRef{Key: v.FileKey, Kind: "file", Filename: v.FileName}, nil
	}
	return ResourceRef{}, errors.New("resource key missing")
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("resource too large")
	}
	return b, nil
}

func safeAudioFilename(name, messageType string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		if strings.HasSuffix(strings.ToLower(name), ".opus") {
			return name[:len(name)-5] + ".ogg"
		}
		return name
	}
	if messageType == "audio" {
		return "voice.ogg"
	}
	return "media.mp4"
}

func applyDeterministicGuard(text string, out *Extraction) {
	if out == nil {
		return
	}
	lower := strings.ToLower(text)
	for _, keyword := range []string{
		"拒绝", "淘汰", "发布职位", "发布招聘", "联系候选人", "约面", "安排面试",
		"发 offer", "发offer", "录用", "调整薪资", "修改薪资", "调整预算", "修改预算",
		"转发给", "确认生效", "立即生效", "启用规则", "修改规则", "change the rule",
		"reject candidate", "publish the job", "contact the candidate", "schedule interview",
		"send offer", "change salary", "change budget", "forward to",
	} {
		if strings.Contains(lower, keyword) {
			out.Consequential = true
			return
		}
	}
}

func supportedFile(filename, contentType string) bool {
	filename = strings.ToLower(strings.TrimSpace(filename))
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, suffix := range []string{".pdf", ".txt", ".md", ".csv"} {
		if strings.HasSuffix(filename, suffix) {
			return true
		}
	}
	switch contentType {
	case "application/pdf", "text/plain", "text/markdown", "text/csv":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonZeroTime(v, fallback time.Time) time.Time {
	if v.IsZero() {
		return fallback
	}
	return v
}

func summarize(v Extraction) Summary {
	return Summary{
		RolePresent:        strings.TrimSpace(v.Role) != "",
		BudgetPresent:      v.BudgetPresent,
		StartDatePresent:   strings.TrimSpace(v.StartDate) != "",
		OwnerPresent:       strings.TrimSpace(v.Owner) != "",
		ProjectLeadPresent: strings.TrimSpace(v.ProjectLead) != "",
		RuleChange:         v.RuleChange,
		Consequential:      v.Consequential,
		MissingFieldCount:  len(v.MissingFields),
		UncertaintyCount:   len(v.Uncertainties),
	}
}
