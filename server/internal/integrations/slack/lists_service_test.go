package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeListsQueries struct {
	installs []db.ChannelInstallation
	messages []db.ChatMessage
}

func (f *fakeListsQueries) ListChannelInstallationsByWorkspace(context.Context, db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return f.installs, nil
}
func (f *fakeListsQueries) ListChatMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	return f.messages, nil
}

type fakeListsAPI struct {
	schema      ListsSchema
	schemaErr   error
	created     ListsItem
	createErr   error
	updated     ListsItem
	updateErr   error
	lastToken   string
	createCalls int
	schemaCalls int
}

func (f *fakeListsAPI) GetSchema(_ context.Context, token, _ string) (ListsSchema, error) {
	f.schemaCalls++
	f.lastToken = token
	return f.schema, f.schemaErr
}
func (f *fakeListsAPI) CreateItem(_ context.Context, token, _ string, _ []map[string]any) (ListsItem, error) {
	f.createCalls++
	f.lastToken = token
	return f.created, f.createErr
}
func (f *fakeListsAPI) UpdateItem(_ context.Context, token, _, _ string, _ []map[string]any) (ListsItem, error) {
	f.lastToken = token
	return f.updated, f.updateErr
}

func slackInstallConfigWithAllowlist(ids ...string) []byte {
	b, _ := json.Marshal(map[string]any{
		"app_id":              "T1",
		"bot_user_id":         "UBOT",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte("xoxb-test")),
		"lists_allowlist":     ids,
	})
	return b
}

func testListsService(q *fakeListsQueries, api *fakeListsAPI, decrypt Decrypter) *ListsService {
	if len(q.installs) == 0 {
		q.installs = []db.ChannelInstallation{{
			AgentID: uid(3),
			Status:  "active",
			Config:  slackInstallConfigWithAllowlist(defaultIdeaListID, defaultFeatureListID),
		}}
	}
	return &ListsService{
		q:       q,
		decrypt: decrypt,
		api:     api,
		policy:  listsPolicyFromEnv(func(string) string { return "" }),
	}
}

func TestListsServiceCreateRequiresIdeaCommand(t *testing.T) {
	api := &fakeListsAPI{
		schema:  ListsSchema{ListID: defaultIdeaListID, Columns: []ListsColumn{{ID: "ColT", Name: "Title", IsPrimaryColumn: true}}},
		created: ListsItem{ID: "Rec1", ListID: defaultIdeaListID, Title: "lamp"},
	}
	q := &fakeListsQueries{
		messages: []db.ChatMessage{{Role: "user", Content: "just chatting about lamps"}},
	}
	s := testListsService(q, api, nil)
	_, err := s.Create(context.Background(), uid(1), uid(3), uid(9), defaultIdeaListID, []ListsField{{Text: "lamp"}})
	if !errors.Is(err, ErrListsWriteNotAuthorized) {
		t.Fatalf("ordinary chat write: %v", err)
	}
	if api.createCalls != 0 {
		t.Fatal("Slack create must not run for ordinary chat")
	}

	q.messages = []db.ChatMessage{{Role: "user", Content: "/idea hallway lamp"}}
	item, err := s.Create(context.Background(), uid(1), uid(3), uid(9), defaultIdeaListID, []ListsField{{Text: "lamp"}})
	if err != nil {
		t.Fatalf("idea write: %v", err)
	}
	if item.ID != "Rec1" {
		t.Fatalf("item = %+v", item)
	}
	if api.lastToken != "xoxb-test" {
		t.Fatalf("token passed to Slack = %q", api.lastToken)
	}
}

func TestListsServiceRejectsWrongListAndMissingInstall(t *testing.T) {
	api := &fakeListsAPI{schema: ListsSchema{Columns: []ListsColumn{{ID: "C", IsPrimaryColumn: true}}}}
	q := &fakeListsQueries{
		messages: []db.ChatMessage{{Role: "user", Content: "/idea x"}},
	}
	s := testListsService(q, api, nil)

	_, err := s.Create(context.Background(), uid(1), uid(3), uid(9), defaultFeatureListID, []ListsField{{Text: "x"}})
	if !errors.Is(err, ErrListsCommandMismatch) {
		t.Fatalf("idea→feature list: %v", err)
	}

	_, err = s.Schema(context.Background(), uid(1), uid(3), uid(9), "F999")
	if !errors.Is(err, ErrListsListNotAllowed) {
		t.Fatalf("off-allowlist schema: %v", err)
	}
	if api.schemaCalls != 0 {
		t.Fatal("off-allowlist must not call Slack")
	}

	_, err = s.Schema(context.Background(), uid(1), uid(8), uid(9), defaultIdeaListID)
	if !errors.Is(err, ErrListsNotConfigured) {
		t.Fatalf("other agent: %v", err)
	}
}

func TestListsServiceAllowlistCheckedBeforeDecrypt(t *testing.T) {
	decrypt := Decrypter(func([]byte) ([]byte, error) {
		t.Fatal("must not decrypt token for an off-allowlist list")
		return nil, errors.New("no")
	})
	s := testListsService(&fakeListsQueries{}, &fakeListsAPI{}, decrypt)
	_, err := s.Schema(context.Background(), uid(1), uid(3), uid(9), "F0BPZZZZZZZ")
	if !errors.Is(err, ErrListsListNotAllowed) {
		t.Fatalf("got %v", err)
	}
}

func TestListsServiceRevokedAndEmptyAllowlist(t *testing.T) {
	api := &fakeListsAPI{}
	q := &fakeListsQueries{installs: []db.ChannelInstallation{{
		AgentID: uid(3),
		Status:  "revoked",
		Config:  slackInstallConfigWithAllowlist(defaultIdeaListID),
	}}}
	s := testListsService(q, api, nil)
	_, err := s.Schema(context.Background(), uid(1), uid(3), uid(9), defaultIdeaListID)
	if !errors.Is(err, ErrListsNotConfigured) {
		t.Fatalf("revoked: %v", err)
	}

	q.installs = []db.ChannelInstallation{{
		AgentID: uid(3),
		Status:  "active",
		Config:  slackInstallConfigJSON(),
	}}
	_, err = s.Schema(context.Background(), uid(1), uid(3), uid(9), defaultIdeaListID)
	if !errors.Is(err, ErrListsListNotAllowed) {
		t.Fatalf("empty allowlist: %v", err)
	}
}

func TestListsServiceRedactsTokenOnAPIError(t *testing.T) {
	api := &fakeListsAPI{schemaErr: errors.New("upstream rejected xoxb-should-never-leak")}
	s := testListsService(&fakeListsQueries{}, api, nil)
	_, err := s.Schema(context.Background(), uid(1), uid(3), uid(9), defaultIdeaListID)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "xoxb-") {
		t.Fatalf("token leaked: %v", err)
	}
}
