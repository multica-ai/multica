package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const outboundMaxUTF8Bytes = 20480

// outboundPayload is the v1 queue payload shape (spec §5.3.4).
type outboundPayload struct {
	Template        string `json:"template,omitempty"`
	Content         string `json:"content,omitempty"`
	FailureReason   string `json:"failure_reason,omitempty"`
	AgentName       string `json:"agent_name,omitempty"`
	IssueID         string `json:"issue_id,omitempty"`
	IssueIdentifier string `json:"issue_identifier,omitempty"`
	IssueNumber     int32  `json:"issue_number,omitempty"`
	IssueTitle      string `json:"issue_title,omitempty"`
}

// RenderInput supplies locale, link bases, and optional session scope for one
// outbound render pass.
type RenderInput struct {
	Locale          string
	AppURL          string
	WorkspaceSlug   string
	ChatSessionID   string
	BindingTokenRaw string
}

type localizedStrings struct {
	BindingPrompt      string
	BindingPromptGroup string
	WelcomeBound       string
	WelcomeUnbound     string
	AgentOffline       string
	AgentArchived      string
	TaskFailed         string
	IssueCreated       string
	OverLimitSuffix    string
}

var renderCatalog = map[string]localizedStrings{
	"en": {
		BindingPrompt:      "Link your Multica account to continue:\n%s",
		BindingPromptGroup: "Please message this bot in a direct chat to link your Multica account.",
		WelcomeBound:       "Welcome to Multica! Send a message or use `/issue` to create a task.",
		WelcomeUnbound:     "Welcome to Multica! Link your account to get started:\n%s",
		AgentOffline:       "This agent is offline right now. Try again later.",
		AgentArchived:      "This agent has been archived and can no longer reply.",
		TaskFailed:         "The agent run failed. Open Multica for details.",
		IssueCreated:       "Created issue **%s**: %s",
		OverLimitSuffix:    "\n\n… (reply truncated — open Multica for the full message: %s)",
	},
	"zh-Hans": {
		BindingPrompt:      "绑定 Multica 账号以继续：\n%s",
		BindingPromptGroup: "请在机器人单聊中绑定 Multica 账号。",
		WelcomeBound:       "欢迎使用 Multica！发送消息或使用 `/issue` 创建任务。",
		WelcomeUnbound:     "欢迎使用 Multica！绑定账号以开始使用：\n%s",
		AgentOffline:       "智能体当前离线，请稍后再试。",
		AgentArchived:      "智能体已归档，无法继续回复。",
		TaskFailed:         "智能体执行失败，请在 Multica 中查看详情。",
		IssueCreated:       "已创建任务 **%s**：%s",
		OverLimitSuffix:    "\n\n…（回复已截断，完整内容见 Multica：%s）",
	},
	"ja": {
		BindingPrompt:      "Multica アカウントを連携してください：\n%s",
		BindingPromptGroup: "ボットとの DM で Multica アカウントを連携してください。",
		WelcomeBound:       "Multica へようこそ！メッセージを送るか `/issue` で課題を作成できます。",
		WelcomeUnbound:     "Multica へようこそ！アカウントを連携して始めましょう：\n%s",
		AgentOffline:       "エージェントは現在オフラインです。しばらくしてからお試しください。",
		AgentArchived:      "エージェントはアーカイブ済みで応答できません。",
		TaskFailed:         "エージェントの実行に失敗しました。Multica で詳細を確認してください。",
		IssueCreated:       "課題 **%s** を作成しました：%s",
		OverLimitSuffix:    "\n\n…（省略 — 全文は Multica：%s）",
	},
	"ko": {
		BindingPrompt:      "Multica 계정을 연결해 주세요:\n%s",
		BindingPromptGroup: "봇과 DM으로 Multica 계정을 연결해 주세요.",
		WelcomeBound:       "Multica에 오신 것을 환영합니다! 메시지를 보내거나 `/issue`로 이슈를 만드세요.",
		WelcomeUnbound:     "Multica에 오신 것을 환영합니다! 계정을 연결해 시작하세요:\n%s",
		AgentOffline:       "에이전트가 오프라인입니다. 나중에 다시 시도해 주세요.",
		AgentArchived:      "에이전트가 보관되어 더 이상 응답할 수 없습니다.",
		TaskFailed:         "에이전트 실행에 실패했습니다. Multica에서 자세히 확인하세요.",
		IssueCreated:       "이슈 **%s**을(를) 만들었습니다: %s",
		OverLimitSuffix:    "\n\n… (잘림 — 전체 내용은 Multica: %s)",
	},
}

// WelcomeInput supplies locale and optional bind-link material for enter_chat
// welcome copy. Bound welcomes never include a token (spec §5.3.2 exception).
type WelcomeInput struct {
	Locale          string
	AppURL          string
	Bound           bool
	BindingTokenRaw string
}

// RenderWelcome builds the markdown body for aibot_respond_welcome_msg.
func RenderWelcome(in WelcomeInput) (string, error) {
	locale := NormalizeLocale(in.Locale)
	texts := renderCatalog[locale]
	if in.Bound {
		return texts.WelcomeBound, nil
	}
	link := bindingURL(in.AppURL, in.BindingTokenRaw)
	if link == "" {
		return "", errors.New("wecom render: binding token required for unbound welcome")
	}
	return fmt.Sprintf(texts.WelcomeUnbound, link), nil
}

// RenderOutbound turns a queued payload into WeCom markdown (spec §5.3.4).
func RenderOutbound(rawPayload []byte, in RenderInput) (string, error) {
	if len(rawPayload) == 0 {
		return "", errors.New("wecom render: empty payload")
	}
	var payload outboundPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return "", fmt.Errorf("wecom render: decode payload: %w", err)
	}
	body, err := renderBody(payload, in)
	if err != nil {
		return "", err
	}
	return TruncateOutboundMarkdown(body, in), nil
}

func renderBody(payload outboundPayload, in RenderInput) (string, error) {
	locale := NormalizeLocale(in.Locale)
	texts := renderCatalog[locale]
	switch payload.Template {
	case "":
		if strings.TrimSpace(payload.Content) == "" {
			return "", errors.New("wecom render: empty content")
		}
		return payload.Content, nil
	case templateBindingPrompt:
		link := bindingURL(in.AppURL, in.BindingTokenRaw)
		if link == "" {
			return "", errors.New("wecom render: binding token required")
		}
		return fmt.Sprintf(texts.BindingPrompt, link), nil
	case templateBindingPromptGroup:
		return texts.BindingPromptGroup, nil
	case templateAgentOffline:
		return texts.AgentOffline, nil
	case templateAgentArchived:
		return texts.AgentArchived, nil
	case templateIssueCreated:
		id := payload.IssueIdentifier
		if id == "" && payload.IssueNumber > 0 {
			id = fmt.Sprintf("#%d", payload.IssueNumber)
		}
		title := strings.TrimSpace(payload.IssueTitle)
		if title == "" {
			title = payload.IssueID
		}
		return fmt.Sprintf(texts.IssueCreated, id, title), nil
	case "task_failed":
		if payload.AgentName != "" {
			return fmt.Sprintf("%s — %s", payload.AgentName, texts.TaskFailed), nil
		}
		return texts.TaskFailed, nil
	default:
		return "", fmt.Errorf("wecom render: unknown template %q", payload.Template)
	}
}

func bindingURL(appURL, rawToken string) string {
	appURL = strings.TrimRight(strings.TrimSpace(appURL), "/")
	rawToken = strings.TrimSpace(rawToken)
	if appURL == "" || rawToken == "" {
		return ""
	}
	u, err := url.Parse(appURL + "/wecom/bind")
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("token", rawToken)
	u.RawQuery = q.Encode()
	return u.String()
}

func chatDeepLink(in RenderInput) string {
	appURL := strings.TrimRight(strings.TrimSpace(in.AppURL), "/")
	slug := strings.TrimSpace(in.WorkspaceSlug)
	sessionID := strings.TrimSpace(in.ChatSessionID)
	if appURL == "" || slug == "" || sessionID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/chat?session=%s", appURL, slug, url.QueryEscape(sessionID))
}

// TruncateOutboundMarkdown applies the 20480 UTF-8 byte cap with a localized
// suffix link when session scope is available (spec §5.3.2).
func TruncateOutboundMarkdown(body string, in RenderInput) string {
	if len(body) <= outboundMaxUTF8Bytes {
		return body
	}
	locale := NormalizeLocale(in.Locale)
	suffixTpl := renderCatalog[locale].OverLimitSuffix
	link := chatDeepLink(in)
	suffix := ""
	if link != "" {
		suffix = fmt.Sprintf(suffixTpl, link)
	}
	maxBody := outboundMaxUTF8Bytes - len(suffix)
	if maxBody < 0 {
		maxBody = 0
	}
	return TruncateUTF8Bytes(body, maxBody) + suffix
}

// TruncateUTF8Bytes shortens s to at most maxBytes without splitting a rune.
func TruncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// Walk runes until adding the next would exceed maxBytes.
	end := 0
	for _, r := range s {
		size := utf8.RuneLen(r)
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return s[:end]
}
