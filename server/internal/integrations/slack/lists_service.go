package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	// ErrListsNotConfigured is returned when this agent has no active Slack app.
	ErrListsNotConfigured = errors.New("slack lists: not configured")
	// ErrListsListNotAllowed is an off-allowlist list_id. Checked before the
	// bot token is decrypted.
	ErrListsListNotAllowed = errors.New("slack lists: list_id is not on the allowlist")
	// ErrListsWriteNotAuthorized is ordinary chat (no /idea or /feature).
	ErrListsWriteNotAuthorized = errors.New("slack lists: writes are only allowed on /idea or /feature")
	// ErrListsCommandMismatch is /idea targeting the feature list (or vice versa).
	ErrListsCommandMismatch = errors.New("slack lists: list_id does not match the current /idea or /feature command")
)

// listsQueries is the DB slice ListsService needs. *db.Queries satisfies it.
type listsQueries interface {
	ListChannelInstallationsByWorkspace(ctx context.Context, arg db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error)
	ListChatMessages(ctx context.Context, chatSessionID pgtype.UUID) ([]db.ChatMessage, error)
}

// listsAPI is the Slack HTTP surface. *ListsClient satisfies it.
type listsAPI interface {
	GetSchema(ctx context.Context, token, listID string) (ListsSchema, error)
	CreateItem(ctx context.Context, token, listID string, fields []map[string]any) (ListsItem, error)
	UpdateItem(ctx context.Context, token, listID, itemID string, fields []map[string]any) (ListsItem, error)
}

// ListsService runs allowlisted Slack Lists reads/writes for the agent that
// owns the current Slack installation. The bot token is decrypted in-process
// and used only as an Authorization header — it never appears in return values.
type ListsService struct {
	q       listsQueries
	decrypt Decrypter
	api     listsAPI
	policy  ListsPolicy
	logger  *slog.Logger
}

// NewListsService wires the service. policy is typically LoadListsPolicy().
func NewListsService(q *db.Queries, decrypt Decrypter, logger *slog.Logger, policy ListsPolicy) *ListsService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ListsService{
		q:       q,
		decrypt: decrypt,
		api:     &ListsClient{},
		policy:  policy,
		logger:  logger,
	}
}

func (s *ListsService) Schema(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID string) (ListsSchema, error) {
	tok, err := s.resolveToken(ctx, workspaceID, agentID, sessionID, listID, false)
	if err != nil {
		return ListsSchema{}, err
	}
	schema, err := s.api.GetSchema(ctx, tok, listID)
	if err != nil {
		return ListsSchema{}, redactErr(err)
	}
	return schema, nil
}

func (s *ListsService) Create(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID string, fields []ListsField) (ListsItem, error) {
	tok, err := s.resolveToken(ctx, workspaceID, agentID, sessionID, listID, true)
	if err != nil {
		return ListsItem{}, err
	}
	schema, err := s.api.GetSchema(ctx, tok, listID)
	if err != nil {
		return ListsItem{}, redactErr(err)
	}
	encoded, title, err := EncodeListsFields(fields, schema)
	if err != nil {
		return ListsItem{}, err
	}
	item, err := s.api.CreateItem(ctx, tok, listID, encoded)
	if err != nil {
		return ListsItem{}, redactErr(err)
	}
	if item.Title == "" {
		item.Title = title
	}
	return item, nil
}

func (s *ListsService) Update(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID, itemID string, fields []ListsField) (ListsItem, error) {
	if strings.TrimSpace(itemID) == "" {
		return ListsItem{}, errors.New("slack lists: item_id is required")
	}
	tok, err := s.resolveToken(ctx, workspaceID, agentID, sessionID, listID, true)
	if err != nil {
		return ListsItem{}, err
	}
	schema, err := s.api.GetSchema(ctx, tok, listID)
	if err != nil {
		return ListsItem{}, redactErr(err)
	}
	encoded, title, err := EncodeListsFields(fields, schema)
	if err != nil {
		return ListsItem{}, err
	}
	item, err := s.api.UpdateItem(ctx, tok, listID, itemID, encoded)
	if err != nil {
		return ListsItem{}, redactErr(err)
	}
	if item.Title == "" {
		item.Title = title
	}
	return item, nil
}

func (s *ListsService) resolveToken(ctx context.Context, workspaceID, agentID, sessionID pgtype.UUID, listID string, write bool) (string, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return "", errors.New("slack lists: list_id is required")
	}
	if !workspaceID.Valid || !agentID.Valid {
		return "", ErrListsNotConfigured
	}
	rows, err := s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: workspaceID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		return "", fmt.Errorf("list slack installations: %w", err)
	}
	var inst *db.ChannelInstallation
	for i := range rows {
		if rows[i].AgentID == agentID && rows[i].Status == "active" {
			inst = &rows[i]
			break
		}
	}
	if inst == nil {
		return "", ErrListsNotConfigured
	}
	// Fail closed before decrypting the bot token.
	if !listIDAllowed(listsAllowlistFromConfig(inst.Config), listID) {
		return "", ErrListsListNotAllowed
	}
	if write {
		if err := s.authorizeWrite(ctx, sessionID, listID); err != nil {
			return "", err
		}
	}
	creds, err := decodeCredentials(inst.Config, s.decrypt)
	if err != nil {
		return "", fmt.Errorf("decode slack credentials: %w", redactErr(err))
	}
	if strings.TrimSpace(creds.BotToken) == "" {
		return "", ErrListsNotConfigured
	}
	return creds.BotToken, nil
}

func (s *ListsService) authorizeWrite(ctx context.Context, sessionID pgtype.UUID, listID string) error {
	if !sessionID.Valid {
		return ErrListsWriteNotAuthorized
	}
	msgs, err := s.q.ListChatMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("read chat messages: %w", err)
	}
	var latest string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && strings.TrimSpace(msgs[i].Content) != "" {
			latest = msgs[i].Content
			break
		}
	}
	cmd, _ := ParseListsCommand(latest)
	if cmd == ListsCommandNone {
		return ErrListsWriteNotAuthorized
	}
	want := s.policy.ListIDFor(cmd)
	if want == "" || want != listID {
		return ErrListsCommandMismatch
	}
	return nil
}

func redactErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(slackAPIError); ok {
		return err
	}
	return errors.New(redactSlackSecrets(err.Error()))
}
