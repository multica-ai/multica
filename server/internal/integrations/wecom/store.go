package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// SupportedLocales enumerates the four installation locales spec §3.2 pins the
// render.go text map to. The locale is snapshotted from the initiator's
// `user.language` at finalize; anything else falls back to "en" so an
// unfamiliar future value cannot leave a bot without system copy.
var SupportedLocales = []string{"en", "zh-Hans", "ja", "ko"}

// DefaultLocale is the fallback for unknown user.language values (spec §3.2).
const DefaultLocale = "en"

// NormalizeLocale collapses arbitrary user.language values onto the four
// supported installation locales. Empty and unknown inputs return
// DefaultLocale; the caller does NOT need to defend against nil users.
//
// Match order is exact first, then a light case-fold plus common Chinese
// aliases so a browser-detected `zh-CN` still lands on `zh-Hans`. Everything
// else is treated as a foreign locale and reported as "en" without warning —
// the render map controls system copy, not agent-authored replies.
func NormalizeLocale(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return DefaultLocale
	}
	for _, supported := range SupportedLocales {
		if strings.EqualFold(trimmed, supported) {
			return supported
		}
	}
	// Common Chinese aliases → zh-Hans.
	lower := strings.ToLower(trimmed)
	switch lower {
	case "zh", "zh-cn", "zh_cn", "zh-hans", "zh_hans":
		return "zh-Hans"
	}
	return DefaultLocale
}

// InstallationConfig mirrors the channel_installation.config JSONB blob for a
// WeCom installation (spec §3.2). AppID stores the wire's `bot_id` /
// `aibotid` so the global unique index idx_channel_installation_type_appid
// keeps its cross-workspace routing invariant intact. SecretEncrypted is
// base64-encoded secretbox ciphertext; Locale is one of SupportedLocales.
//
// The struct is intentionally shared with wecom_channel.go's decoder — every
// producer / consumer of the config blob speaks the same shape.
type InstallationConfig struct {
	AppID           string `json:"app_id"`
	SecretEncrypted string `json:"secret_encrypted"`
	Locale          string `json:"locale"`
}

// Marshal serializes cfg to the JSON shape channel_installation.config
// expects. Validation is enforced up front so a caller cannot commit a row
// missing the routing key (app_id) or the ciphertext.
func (cfg InstallationConfig) Marshal() ([]byte, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("wecom: InstallationConfig.AppID is required")
	}
	if strings.TrimSpace(cfg.SecretEncrypted) == "" {
		return nil, errors.New("wecom: InstallationConfig.SecretEncrypted is required")
	}
	locale := NormalizeLocale(cfg.Locale)
	return json.Marshal(InstallationConfig{
		AppID:           cfg.AppID,
		SecretEncrypted: cfg.SecretEncrypted,
		Locale:          locale,
	})
}

// UnmarshalInstallationConfig decodes the raw config blob. Callers that only
// need the app_id (e.g. list handlers) can accept a partial parse; missing
// SecretEncrypted is tolerated so the wire response does not have to
// synthesize a placeholder.
func UnmarshalInstallationConfig(raw []byte) (InstallationConfig, error) {
	var cfg InstallationConfig
	if len(raw) == 0 {
		return cfg, errors.New("wecom: installation config is empty")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("wecom: decode installation config: %w", err)
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return cfg, errors.New("wecom: installation config missing app_id")
	}
	return cfg, nil
}

// Target chat_type wire values (spec §5.4). Conversion is centralized here;
// other files must not scatter magic numbers.
const (
	TargetChatTypeP2P   int16 = 1
	TargetChatTypeGroup int16 = 2
)

// wecomBindingConfig is persisted on channel_chat_session_binding.config so
// outbound delivery can route without re-parsing inbound payloads (spec §5.4).
type wecomBindingConfig struct {
	TargetChatID   string `json:"target_chat_id"`
	TargetChatType int16  `json:"target_chat_type"`
}

// ChatTypeToTarget maps the normalized ChatType to WeCom's target_chat_type.
func ChatTypeToTarget(ct channel.ChatType) (int16, error) {
	switch ct {
	case channel.ChatTypeP2P:
		return TargetChatTypeP2P, nil
	case channel.ChatTypeGroup:
		return TargetChatTypeGroup, nil
	default:
		return 0, fmt.Errorf("wecom: unknown chat type %q", ct)
	}
}

// TargetToChatType maps WeCom target_chat_type back to ChatType.
func TargetToChatType(t int16) (channel.ChatType, error) {
	switch t {
	case TargetChatTypeP2P:
		return channel.ChatTypeP2P, nil
	case TargetChatTypeGroup:
		return channel.ChatTypeGroup, nil
	default:
		return "", fmt.Errorf("wecom: unknown target_chat_type %d", t)
	}
}

// OutboundTarget derives the WeCom send target from one inbound message
// (spec §5.4): p2p replies to from.userid; group replies to chatid.
func OutboundTarget(msg channel.InboundMessage) (targetChatID string, targetChatType int16, err error) {
	switch msg.Source.ChatType {
	case channel.ChatTypeP2P:
		if msg.Source.SenderID == "" {
			return "", 0, errors.New("wecom: p2p inbound missing sender id")
		}
		return msg.Source.SenderID, TargetChatTypeP2P, nil
	case channel.ChatTypeGroup:
		if msg.Source.ChatID == "" {
			return "", 0, errors.New("wecom: group inbound missing chat id")
		}
		return msg.Source.ChatID, TargetChatTypeGroup, nil
	default:
		return "", 0, fmt.Errorf("wecom: unknown chat type %q", msg.Source.ChatType)
	}
}

// SessionBinding derives the session-isolation key and binding config from one
// inbound message (spec §5.4).
func SessionBinding(msg channel.InboundMessage) (bindingKey string, config []byte, err error) {
	targetID, targetType, err := OutboundTarget(msg)
	if err != nil {
		return "", nil, err
	}
	cfg, err := json.Marshal(wecomBindingConfig{
		TargetChatID:   targetID,
		TargetChatType: targetType,
	})
	if err != nil {
		return "", nil, fmt.Errorf("wecom: marshal binding config: %w", err)
	}
	switch msg.Source.ChatType {
	case channel.ChatTypeP2P:
		return msg.Source.SenderID, cfg, nil
	case channel.ChatTypeGroup:
		return msg.Source.ChatID, cfg, nil
	default:
		return "", nil, fmt.Errorf("wecom: unknown chat type %q", msg.Source.ChatType)
	}
}
