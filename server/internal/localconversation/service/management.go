package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

var (
	// ErrInvalidRequest rejects a management request whose values are not
	// acceptable.
	ErrInvalidRequest = errors.New("local conversation: invalid request")
	// ErrConversationStarted rejects changing the working directory after
	// the first message.
	ErrConversationStarted = errors.New("local conversation: choose a working directory before sending the first message")
)

// DefaultTitle is the title of a conversation nobody has named yet.
const DefaultTitle = "新会话"

// CreateCommand creates a conversation. At most one of FolderProjectID and
// DirectorySourceID chooses its working directory; without either it uses
// the scope's shared default project directory.
type CreateCommand struct {
	ScopeID           int64
	ID                string
	Provider          string
	Title             string
	ProjectID         *string
	FolderProjectID   string
	DirectorySourceID string
}

// Create creates a conversation and returns it.
func (s *Service) Create(ctx context.Context, cmd CreateCommand) (sqlitedb.Conversation, error) {
	if cmd.ID == "" || len(cmd.ID) > 128 {
		return sqlitedb.Conversation{}, fmt.Errorf("%w: conversation id", ErrInvalidRequest)
	}
	if (cmd.Provider == "claude" && s.opts.Claude == nil) || (cmd.Provider == "codex" && s.opts.Codex == nil) || (cmd.Provider != "claude" && cmd.Provider != "codex") {
		return sqlitedb.Conversation{}, fmt.Errorf("%w: provider %q is not available", ErrInvalidRequest, cmd.Provider)
	}
	if cmd.FolderProjectID != "" && cmd.DirectorySourceID != "" {
		return sqlitedb.Conversation{}, fmt.Errorf("%w: choose one directory source", ErrInvalidRequest)
	}
	title, err := conversationTitle(cmd.Title)
	if err != nil {
		return sqlitedb.Conversation{}, err
	}
	mode := "manual"
	if title == DefaultTitle {
		mode = "auto"
	}

	var cwd string
	switch {
	case cmd.FolderProjectID != "":
		project, err := s.store.FolderProject(ctx, cmd.ScopeID, cmd.FolderProjectID)
		if err != nil {
			return sqlitedb.Conversation{}, err
		}
		if cwd, err = projectDirectory(project.Path); err != nil {
			return sqlitedb.Conversation{}, err
		}
		if cwd != project.Path {
			return sqlitedb.Conversation{}, fmt.Errorf("%w: the project folder changed; add the folder again", ErrInvalidRequest)
		}
	case cmd.DirectorySourceID != "":
		source, err := s.scopedConversation(ctx, cmd.ScopeID, cmd.DirectorySourceID)
		if err != nil {
			return sqlitedb.Conversation{}, err
		}
		if cwd, err = projectDirectory(source.Cwd); err != nil {
			return sqlitedb.Conversation{}, err
		}
	default:
		project, err := s.defaultFolderProject(ctx, cmd.ScopeID)
		if err != nil {
			return sqlitedb.Conversation{}, err
		}
		cwd = project.Path
	}

	if _, err := s.store.CreateConversation(ctx, store.NewConversation{
		ID: cmd.ID, ScopeID: cmd.ScopeID, Provider: cmd.Provider, Title: title, TitleMode: mode,
		Cwd: cwd, ProjectID: cmd.ProjectID, CreatedAt: s.opts.Now(),
	}); err != nil {
		return sqlitedb.Conversation{}, err
	}
	return s.publishCreated(ctx, cmd.ID)
}

// publishCreated announces a conversation created outside the writer to
// live list subscribers and returns it.
func (s *Service) publishCreated(ctx context.Context, id string) (sqlitedb.Conversation, error) {
	summary, err := s.store.Summary(ctx, id)
	if err != nil {
		return sqlitedb.Conversation{}, err
	}
	s.hub.Publish(id, store.Committed{Summary: &summary})
	return summary.Conversation, nil
}

// MetadataUpdate changes how a conversation is listed. Nil fields keep
// their value; SetProject with a nil ProjectID removes the project.
type MetadataUpdate struct {
	Title      *string
	Pinned     *bool
	Archived   *bool
	SetProject bool
	ProjectID  *string
}

// UpdateMetadata applies a metadata update and records it as a
// conversation.updated event. Naming a conversation makes its title manual.
func (s *Service) UpdateMetadata(ctx context.Context, scopeID int64, conversationID string, u MetadataUpdate) (sqlitedb.Conversation, error) {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return conv, err
	}
	meta := metadataOf(conv)
	changed := map[string]any{}
	batch := store.Batch{ConversationID: conversationID, Metadata: &meta}
	if u.Title != nil {
		title, err := conversationTitle(*u.Title)
		if err != nil {
			return conv, err
		}
		meta.TitleMode = "manual"
		s.cancelTitle(conversationID)
		batch.Summary = &store.SummaryPatch{Title: &title, UpdatedAt: s.opts.Now()}
		changed["title"], changed["title_mode"] = title, "manual"
	}
	if u.Pinned != nil {
		meta.Pinned = *u.Pinned
		changed["pinned"] = meta.Pinned
	}
	if u.Archived != nil {
		meta.Archived = *u.Archived
		changed["archived"] = meta.Archived
	}
	if u.SetProject {
		if u.ProjectID != nil && (*u.ProjectID == "" || len(*u.ProjectID) > 128) {
			return conv, fmt.Errorf("%w: project", ErrInvalidRequest)
		}
		meta.ProjectID = u.ProjectID
		changed["project_id"] = u.ProjectID
	}
	if len(changed) == 0 {
		return conv, nil
	}
	payload, _ := json.Marshal(changed)
	batch.Events = []store.NewEvent{{Type: "conversation.updated", Payload: payload, CreatedAt: s.opts.Now()}}
	if _, err := s.batcher.AppendWait(ctx, batch); err != nil {
		return conv, err
	}
	return s.store.Conversation(ctx, conversationID)
}

// SetDirectory moves a conversation that has not started to another
// working directory.
func (s *Service) SetDirectory(ctx context.Context, scopeID int64, conversationID, dir string) (sqlitedb.Conversation, error) {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return conv, err
	}
	s.mu.Lock()
	running := s.runners[conversationID] != nil
	s.mu.Unlock()
	if conv.TurnCount > 0 || running {
		return conv, ErrConversationStarted
	}
	cwd, err := projectDirectory(dir)
	if err != nil {
		return conv, err
	}
	meta := metadataOf(conv)
	meta.Cwd = cwd
	payload, _ := json.Marshal(map[string]any{"cwd": cwd})
	if _, err := s.batcher.AppendWait(ctx, store.Batch{ConversationID: conversationID, Metadata: &meta,
		Events: []store.NewEvent{{Type: "conversation.updated", Payload: payload, CreatedAt: s.opts.Now()}}}); err != nil {
		return conv, err
	}
	return s.store.Conversation(ctx, conversationID)
}

// FolderProjects includes the scope's default project even before its first conversation.
func (s *Service) FolderProjects(ctx context.Context, scopeID int64) ([]sqlitedb.FolderProject, error) {
	if _, err := s.defaultFolderProject(ctx, scopeID); err != nil {
		return nil, err
	}
	return s.store.FolderProjects(ctx, scopeID)
}

// Default projects reserve unassigned:<scope ID> as their stable identity.
// The desktop folds this project and unmatched historical directories into
// its existing unassigned group, without changing those conversations' cwd.
func (s *Service) defaultFolderProject(ctx context.Context, scopeID int64) (sqlitedb.FolderProject, error) {
	if _, err := s.store.ScopeByID(ctx, scopeID); err != nil {
		return sqlitedb.FolderProject{}, err
	}
	id := fmt.Sprintf("unassigned:%d", scopeID)
	project, err := s.store.FolderProject(ctx, scopeID, id)
	if errors.Is(err, store.ErrNotFound) {
		dir := filepath.Join(filepath.Dir(s.opts.Path), "projects", fmt.Sprint(scopeID), "unassigned")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return sqlitedb.FolderProject{}, err
		}
		path, err := projectDirectory(dir)
		if err != nil {
			return sqlitedb.FolderProject{}, err
		}
		return s.store.AddFolderProject(ctx, scopeID, id, "未分配项目", path, s.opts.Now())
	}
	if err != nil {
		return sqlitedb.FolderProject{}, err
	}
	path, err := projectDirectory(project.Path)
	if err != nil {
		return sqlitedb.FolderProject{}, err
	}
	if path != project.Path {
		return sqlitedb.FolderProject{}, fmt.Errorf("%w: the default project folder changed", ErrInvalidRequest)
	}
	return project, nil
}

// AddFolderProject records a project folder; adding the same folder again
// returns the existing project.
func (s *Service) AddFolderProject(ctx context.Context, scopeID int64, dir string) (sqlitedb.FolderProject, error) {
	path, err := projectDirectory(dir)
	if err != nil {
		return sqlitedb.FolderProject{}, err
	}
	title := filepath.Base(path)
	if title == string(filepath.Separator) || title == "." {
		title = path
	}
	return s.store.AddFolderProject(ctx, scopeID, newID(), title, path, s.opts.Now())
}

func metadataOf(c sqlitedb.Conversation) store.ConversationMetadata {
	m := store.ConversationMetadata{TitleMode: c.TitleMode, TitleGeneration: c.TitleGeneration, Cwd: c.Cwd, Pinned: c.Pinned == 1, Archived: c.Archived == 1}
	if c.ProjectID.Valid {
		id := c.ProjectID.String
		m.ProjectID = &id
	}
	return m
}

func conversationTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return DefaultTitle, nil
	}
	if len([]rune(title)) > 256 {
		return "", fmt.Errorf("%w: the title is longer than 256 characters", ErrInvalidRequest)
	}
	return title, nil
}

// projectDirectory resolves a folder the user chose to the existing
// directory it names.
func projectDirectory(dir string) (string, error) {
	if dir == "" || len(dir) > 8192 || !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: the folder must be an absolute path", ErrInvalidRequest)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%w: the folder %s is not available", ErrInvalidRequest, dir)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a folder", ErrInvalidRequest, dir)
	}
	return resolved, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
