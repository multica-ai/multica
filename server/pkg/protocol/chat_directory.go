package protocol

// DaemonCapabilityChatWorkingDirectoryV1 covers host directory discovery and
// execution of a chat in its explicitly selected directory.
const DaemonCapabilityChatWorkingDirectoryV1 = "chat-working-directory-v1"

// ChatWorkingDirectory binds a user-selected path to the machine that owns it.
type ChatWorkingDirectory struct {
	DaemonID string `json:"daemon_id"`
	Path     string `json:"path"`
}

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type DirectoryListing struct {
	Path       string           `json:"path"`
	ParentPath *string          `json:"parent_path"`
	Entries    []DirectoryEntry `json:"entries"`
	Selectable bool             `json:"selectable"`
}
