package facadeimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ChannelTurnAccess interface {
	ResolveUserID(ctx context.Context, channelName, externalUserID string) (pgtype.UUID, error)
	IsWorkspaceMember(ctx context.Context, userID, workspaceID pgtype.UUID) (bool, error)
}

type TaskBackedChannelTurnClient struct {
	queries *db.Queries
	tasks   *service.TaskService
	access  ChannelTurnAccess
}

func NewTaskBackedChannelTurnClient(queries *db.Queries, tasks *service.TaskService, access ChannelTurnAccess) *TaskBackedChannelTurnClient {
	return &TaskBackedChannelTurnClient{
		queries: queries,
		tasks:   tasks,
		access:  access,
	}
}

func (c *TaskBackedChannelTurnClient) StartAgentTurn(ctx context.Context, req chintent.IntentRequest) (string, error) {
	if c == nil || c.queries == nil || c.tasks == nil {
		return "", fmt.Errorf("channel turn client is not configured")
	}
	workspaceID, err := util.ParseUUID(strings.TrimSpace(req.WorkspaceID))
	if err != nil {
		return "", err
	}
	requesterID, err := c.authorizeRequester(ctx, req, workspaceID)
	if err != nil {
		return "", err
	}
	if req.InboundEventID != "" {
		existing, err := c.queries.GetContextTaskByInboundEvent(ctx, db.GetContextTaskByInboundEventParams{
			ContextType:    service.ChannelTurnContextType,
			InboundEventID: req.InboundEventID,
		})
		if err == nil {
			return util.UUIDToString(existing.ID), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("lookup existing channel turn task: %w", err)
		}
	}
	agent, err := c.selectAgent(ctx, workspaceID, req, protocol.DaemonCapabilityChannelTurn)
	if err != nil {
		return "", err
	}
	task, err := c.tasks.EnqueueChannelTurnTask(ctx, workspaceID, agent.ID, service.ChannelTurnTaskParams{
		Prompt:          chintent.BuildChannelAgentTurnPrompt(req),
		Message:         req.Text,
		RequesterID:     requesterID,
		Channel:         req.Channel,
		ChatID:          req.ChatID,
		ChatType:        req.ChatType,
		SenderID:        req.SenderID,
		SenderName:      req.SenderName,
		InboundEventID:  req.InboundEventID,
		ContextIssueKey: req.ContextIssueKey,
	})
	if err != nil {
		return "", err
	}
	return util.UUIDToString(task.ID), nil
}

func (c *TaskBackedChannelTurnClient) ParseAgentTurnResult(ctx context.Context, taskID string) (string, bool, error) {
	if c == nil || c.queries == nil {
		return "", true, fmt.Errorf("channel turn client is not configured")
	}
	taskUUID, err := util.ParseUUID(strings.TrimSpace(taskID))
	if err != nil {
		return "", true, err
	}
	task, err := c.queries.GetAgentTask(ctx, taskUUID)
	if err != nil {
		return "", true, fmt.Errorf("load channel turn task: %w", err)
	}
	switch task.Status {
	case "completed":
		output, err := taskCompletionOutput(task, "channel turn")
		return output, true, err
	case "failed":
		if task.Error.Valid && strings.TrimSpace(task.Error.String) != "" {
			return "", true, fmt.Errorf("channel turn task failed: %s", task.Error.String)
		}
		return "", true, fmt.Errorf("channel turn task failed")
	case "cancelled":
		return "", true, fmt.Errorf("channel turn task cancelled")
	default:
		return "", false, nil
	}
}

func (c *TaskBackedChannelTurnClient) authorizeRequester(ctx context.Context, req chintent.IntentRequest, workspaceID pgtype.UUID) (string, error) {
	if c.access == nil {
		return "", nil
	}
	connectionID := req.ConnectionID
	if connectionID == "" {
		connectionID = req.Channel
	}
	userID, err := c.access.ResolveUserID(ctx, connectionID, req.SenderID)
	if err != nil {
		return "", fmt.Errorf("resolve channel user: %w", err)
	}
	if !userID.Valid {
		return "", fmt.Errorf("resolve channel user: invalid user id")
	}
	member, err := c.access.IsWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return "", fmt.Errorf("check workspace membership: %w", err)
	}
	if !member {
		return "", fmt.Errorf("sender is not a workspace member")
	}
	return util.UUIDToString(userID), nil
}

const (
	boundChannelAgentUnavailableMessage = "指定智能体当前不可用，或对应运行时不支持群聊语义处理。请换一个智能体，或重启/更新运行时后再试。"
	noChannelAgentAvailableMessage      = "我现在找不到可用的 channel agent，先不继续刷屏。等 agent 恢复后你可以再发一次。"
)

func channelAgentUnavailable(message, reason string) error {
	return &chintent.ChannelAgentUnavailableError{Message: message, Reason: reason}
}

func boundChannelAgentUnavailable(reason string) error {
	return channelAgentUnavailable(boundChannelAgentUnavailableMessage, reason)
}

func noChannelAgentAvailable(reason string) error {
	return channelAgentUnavailable(noChannelAgentAvailableMessage, reason)
}

func (c *TaskBackedChannelTurnClient) selectAgent(ctx context.Context, workspaceID pgtype.UUID, req chintent.IntentRequest, capability string) (db.Agent, error) {
	if aid := strings.TrimSpace(req.AgentID); aid != "" {
		agentUUID, err := util.ParseUUID(aid)
		if err != nil {
			return db.Agent{}, boundChannelAgentUnavailable("bound agent id is not a valid UUID")
		}
		agent, err := c.queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          agentUUID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Agent{}, boundChannelAgentUnavailable("bound agent was not found in workspace")
			}
			return db.Agent{}, fmt.Errorf("load bound agent: %w", err)
		}
		if agent.ArchivedAt.Valid {
			return db.Agent{}, boundChannelAgentUnavailable("bound agent is archived")
		}
		if !agent.RuntimeID.Valid {
			return db.Agent{}, boundChannelAgentUnavailable("bound agent has no runtime")
		}
		runtime, err := c.queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return db.Agent{}, boundChannelAgentUnavailable("bound agent runtime was not found")
			}
			return db.Agent{}, fmt.Errorf("load bound agent runtime: %w", err)
		}
		if runtime.Status != "online" {
			return db.Agent{}, boundChannelAgentUnavailable(fmt.Sprintf("bound agent runtime is %s", runtime.Status))
		}
		if !runtimeSupports(runtime, capability) {
			return db.Agent{}, boundChannelAgentUnavailable(fmt.Sprintf("bound agent runtime does not advertise %q", capability))
		}
		return agent, nil
	}

	agents, err := c.queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("list agents: %w", err)
	}
	for _, agent := range agents {
		if !agent.RuntimeID.Valid {
			continue
		}
		runtime, err := c.queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if err != nil || runtime.Status != "online" || !runtimeSupports(runtime, capability) {
			continue
		}
		return agent, nil
	}
	return db.Agent{}, noChannelAgentAvailable(fmt.Sprintf("no online runtime advertises %q", capability))
}

func runtimeSupports(runtime db.AgentRuntime, capability string) bool {
	if capability == "" {
		return true
	}
	var metadata struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(runtime.Metadata, &metadata); err != nil {
		return false
	}
	for _, c := range metadata.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func taskCompletionOutput(task db.AgentTaskQueue, label string) (string, error) {
	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(task.Result, &payload); err != nil {
		return "", fmt.Errorf("parse %s task result: %w", label, err)
	}
	output := strings.TrimSpace(payload.Output)
	if output == "" {
		return "", fmt.Errorf("%s task completed without output", label)
	}
	return output, nil
}

var _ chintent.ChannelAgentTurnClient = (*TaskBackedChannelTurnClient)(nil)
