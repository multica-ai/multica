package protocol

import "regexp"

// FeishuSource is the platform identity of the Feishu/Lark message the current
// chat task answers, frozen on the task's delivery when it was enqueued. Every
// field is a Feishu id, never a Multica id, and none is a credential.
type FeishuSource struct {
	ChatID       string `json:"chat_id"`
	ChatType     string `json:"chat_type,omitempty"`
	MessageID    string `json:"message_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	SenderOpenID string `json:"sender_open_id,omitempty"`
}

var (
	feishuChatID   = regexp.MustCompile(`^oc_[A-Za-z0-9_-]{1,96}$`)
	feishuMessage  = regexp.MustCompile(`^om_[A-Za-z0-9_-]{1,96}$`)
	feishuThreadID = regexp.MustCompile(`^omt_[A-Za-z0-9_-]{1,96}$`)
	feishuOpenID   = regexp.MustCompile(`^ou_[A-Za-z0-9_-]{1,96}$`)
)

// SafeFeishuSource validates the ids at both ends of the claim boundary. The
// chat id is required; any other field that does not have its Feishu shape is
// dropped rather than repaired, so the agent is never handed a guessed id.
func SafeFeishuSource(source *FeishuSource) *FeishuSource {
	if source == nil || !feishuChatID.MatchString(source.ChatID) {
		return nil
	}
	out := *source
	if out.ChatType != "p2p" && out.ChatType != "group" {
		out.ChatType = ""
	}
	if !feishuMessage.MatchString(out.MessageID) {
		out.MessageID = ""
	}
	if !feishuThreadID.MatchString(out.ThreadID) {
		out.ThreadID = ""
	}
	if !feishuOpenID.MatchString(out.SenderOpenID) {
		out.SenderOpenID = ""
	}
	return &out
}
