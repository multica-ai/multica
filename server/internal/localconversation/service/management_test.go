package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

func TestCreateChoosesTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	own, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "conv-1", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if own.Title != DefaultTitle || own.TitleMode != "auto" {
		t.Fatalf("default title = %q/%q", own.Title, own.TitleMode)
	}
	if info, err := os.Stat(own.Cwd); err != nil || !info.IsDir() || !strings.Contains(own.Cwd, filepath.Join("projects")) {
		t.Fatalf("default cwd %q: %v", own.Cwd, err)
	}

	folder := t.TempDir()
	project, err := f.svc.AddFolderProject(ctx, f.scopeID, folder)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := f.svc.AddFolderProject(ctx, f.scopeID, folder)
	if again.ID != project.ID {
		t.Fatal("adding the same folder twice created two projects")
	}
	inFolder, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "conv-2", Provider: "claude", Title: " Plan ", FolderProjectID: project.ID})
	if err != nil || inFolder.Cwd != project.Path || inFolder.Title != "Plan" || inFolder.TitleMode != "manual" {
		t.Fatalf("folder conversation = %+v, %v", inFolder, err)
	}
	shared, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "conv-3", Provider: "claude", DirectorySourceID: "conv-2"})
	if err != nil || shared.Cwd != project.Path {
		t.Fatalf("shared directory = %+v, %v", shared, err)
	}

	for _, bad := range []CreateCommand{
		{ScopeID: f.scopeID, ID: "x", Provider: "codex"},
		{ScopeID: f.scopeID, ID: "x", Provider: "claude", FolderProjectID: project.ID, DirectorySourceID: "conv-2"},
		{ScopeID: f.scopeID, ID: "x", Provider: "claude", Title: strings.Repeat("长", 257)},
	} {
		if _, err := f.svc.Create(ctx, bad); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Create(%+v) = %v", bad, err)
		}
	}
	if _, err := f.svc.AddFolderProject(ctx, f.scopeID, "relative/dir"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("relative folder = %v", err)
	}
}

func TestDefaultProjectSharesDirectoryWithoutMovingExistingConversations(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "legacy", "claude")
	first, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "first", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "second", Provider: "claude"})
	if err != nil || second.Cwd != first.Cwd {
		t.Fatalf("default directories differ: %q / %q, %v", first.Cwd, second.Cwd, err)
	}
	legacy, err := f.svc.Store().Conversation(ctx, "legacy")
	if err != nil || legacy.Cwd != "/repo" {
		t.Fatalf("existing conversation changed: %+v, %v", legacy, err)
	}
	otherScope, err := f.svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "u", WorkspaceID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.svc.Create(ctx, CreateCommand{ScopeID: otherScope, ID: "other", Provider: "claude"})
	if err != nil || other.Cwd == first.Cwd {
		t.Fatalf("workspace directories not isolated: %+v, %v", other, err)
	}
}

func TestDefaultProjectSurvivesRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.sqlite")
	var first sqlitedb.FolderProject
	for _, name := range []string{"first open", "reopen"} {
		t.Run(name, func(t *testing.T) {
			svc, err := Open(ctx, Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Close(ctx)
			scopeID, err := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "u", WorkspaceID: "w"})
			if err != nil {
				t.Fatal(err)
			}
			projects, err := svc.FolderProjects(ctx, scopeID)
			if err != nil || len(projects) != 1 {
				t.Fatalf("default projects = %+v, %v", projects, err)
			}
			if first.ID == "" {
				first = projects[0]
			} else if projects[0] != first {
				t.Fatalf("default project changed on restart: %+v / %+v", first, projects[0])
			}
		})
	}
}

func TestDefaultProjectConcurrentInitialization(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			projects, err := f.svc.FolderProjects(context.Background(), f.scopeID)
			if err != nil || len(projects) != 1 {
				t.Errorf("concurrent initialization = %+v, %v", projects, err)
			}
		}()
	}
	wg.Wait()
}

func TestDefaultProjectUnavailableDoesNotCreateElsewhere(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	projects, err := f.svc.FolderProjects(ctx, f.scopeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(projects[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projects[0].Path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.FolderProjects(ctx, f.scopeID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unavailable project list = %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateCommand{ScopeID: f.scopeID, ID: "unavailable", Provider: "claude"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unavailable default directory = %v", err)
	}
	if _, err := f.svc.Store().Conversation(ctx, "unavailable"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed create persisted a conversation: %v", err)
	}
}

func TestUpdateMetadataPublishesTheSummary(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	before, _ := f.svc.Store().Conversation(ctx, "conv-1")

	title, pinned, project := "Renamed", true, "p-1"
	conv, err := f.svc.UpdateMetadata(ctx, f.scopeID, "conv-1", MetadataUpdate{Title: &title, Pinned: &pinned, SetProject: true, ProjectID: &project})
	if err != nil {
		t.Fatal(err)
	}
	if conv.Title != title || conv.TitleMode != "manual" || conv.Pinned != 1 || conv.ProjectID.String != project || conv.SummarySeq <= before.SummarySeq {
		t.Fatalf("after update = %+v", conv)
	}
	conv, err = f.svc.UpdateMetadata(ctx, f.scopeID, "conv-1", MetadataUpdate{SetProject: true})
	if err != nil || conv.ProjectID.Valid || conv.Pinned != 1 {
		t.Fatalf("clearing the project = %+v, %v", conv, err)
	}
	events, _ := f.svc.Store().EventsAfter(ctx, "conv-1", 0, 100, 10)
	if len(events) != 2 || events[0].Type != "conversation.updated" {
		t.Fatalf("events = %+v", events)
	}
}

func TestSetDirectoryOnlyBeforeTheFirstMessage(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	dir := t.TempDir()
	conv, err := f.svc.SetDirectory(ctx, f.scopeID, "conv-1", dir)
	resolved, _ := filepath.EvalSymlinks(dir)
	if err != nil || conv.Cwd != resolved {
		t.Fatalf("set directory = %q, %v", conv.Cwd, err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetDirectory(ctx, f.scopeID, "conv-1", t.TempDir()); !errors.Is(err, ErrConversationStarted) {
		t.Fatalf("set directory after a message = %v", err)
	}
}
