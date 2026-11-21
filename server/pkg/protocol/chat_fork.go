package protocol

const DaemonCapabilityCodexChatForkV1 = "codex-chat-fork-v1"

// CodexChatForkTask is only sent after the server reserves an idle source chat.
type CodexChatForkTask struct {
	SourceChatID               string `json:"source_chat_id"`
	SourceSessionID            string `json:"source_session_id"`
	SourceWorkDir              string `json:"source_work_dir,omitempty"`
	SourceRequiresNativeResume bool   `json:"source_requires_native_resume,omitempty"`
}
