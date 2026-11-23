package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func normalizeChatWorkingDirectory(directory *protocol.ChatWorkingDirectory) (*protocol.ChatWorkingDirectory, error) {
	if directory == nil {
		return nil, nil
	}
	normalized := &protocol.ChatWorkingDirectory{DaemonID: strings.TrimSpace(directory.DaemonID), Path: directory.Path}
	if normalized.DaemonID == "" {
		return nil, errors.New("working_directory: daemon_id is required")
	}
	if normalized.Path == "" || strings.ContainsRune(normalized.Path, '\x00') || !isAbsoluteLocalPath(normalized.Path) {
		return nil, errors.New("working_directory: path must be a nonempty absolute path")
	}
	return normalized, nil
}

// Empty daemonID accepts any bound machine; empty ownerID skips the creation-only owner gate.
func validateChatDirectoryRuntime(runtime db.AgentRuntime, daemonID, ownerID string) error {
	if ownerID != "" && (!runtime.OwnerID.Valid || uuidToString(runtime.OwnerID) != ownerID) {
		return errors.New("only the runtime owner can choose a working directory")
	}
	if runtime.RuntimeMode != "local" {
		return errors.New("working directories require a local runtime")
	}
	if runtime.Status != "online" {
		return errors.New("the working directory runtime is offline")
	}
	if !runtime.DaemonID.Valid || strings.TrimSpace(runtime.DaemonID.String) == "" {
		return errors.New("the runtime has no bound machine")
	}
	if daemonID != "" && daemonID != runtime.DaemonID.String {
		return errors.New("the working directory belongs to a different machine")
	}
	if !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityChatWorkingDirectoryV1) {
		return errors.New("the runtime does not support chat working directories; update its daemon")
	}
	return nil
}

func validateChatDirectoryAgent(agent db.Agent, runtime db.AgentRuntime) error {
	if runtime.Provider != "openclaw" {
		return nil
	}
	var config struct {
		Mode string `json:"mode"`
	}
	if len(agent.RuntimeConfig) > 0 && !bytes.Equal(bytes.TrimSpace(agent.RuntimeConfig), []byte("null")) {
		if err := json.Unmarshal(agent.RuntimeConfig, &config); err != nil {
			return errors.New("invalid agent runtime configuration")
		}
	}
	if strings.TrimSpace(config.Mode) == "gateway" {
		return errors.New("gateway mode does not support a local working directory")
	}
	return nil
}

func decodeStoredChatWorkingDirectory(raw []byte) (*protocol.ChatWorkingDirectory, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var directory protocol.ChatWorkingDirectory
	if err := json.Unmarshal(raw, &directory); err != nil {
		return nil, fmt.Errorf("invalid saved working directory: %w", err)
	}
	normalized, err := normalizeChatWorkingDirectory(&directory)
	if err != nil {
		return nil, fmt.Errorf("invalid saved working directory: %w", err)
	}
	return normalized, nil
}

func chatWorkingDirectoryForResponse(raw []byte) *protocol.ChatWorkingDirectory {
	directory, _ := decodeStoredChatWorkingDirectory(raw)
	return directory
}

func chatWorkingDirectoryForClaim(session db.ChatSession, agent db.Agent, runtime db.AgentRuntime, hasCapability bool) (*protocol.ChatWorkingDirectory, error) {
	directory, err := decodeStoredChatWorkingDirectory(session.WorkingDirectory)
	if err != nil || directory == nil {
		return directory, err
	}
	if !hasCapability {
		return nil, errors.New("the claiming daemon does not support chat working directories; update its daemon")
	}
	if !agent.RuntimeID.Valid || agent.RuntimeID != runtime.ID || agent.WorkspaceID != runtime.WorkspaceID || session.WorkspaceID != runtime.WorkspaceID {
		return nil, errors.New("the agent is no longer bound to the working directory runtime")
	}
	if err := validateChatDirectoryRuntime(runtime, directory.DaemonID, ""); err != nil {
		return nil, err
	}
	if err := validateChatDirectoryAgent(agent, runtime); err != nil {
		return nil, err
	}
	return directory, nil
}
