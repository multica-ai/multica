package agentmemory

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// fakeQuerier records calls and returns scripted results so the default-off and
// both-off-deletes logic can be tested without a database.
type fakeQuerier struct {
	getErr     error
	getRow     cerebrodb.GetUserAgentMemorySettingsRow
	upsertRow  cerebrodb.UpsertUserAgentMemorySettingsRow
	upserted   bool
	deleted    bool
	upsertArgs cerebrodb.UpsertUserAgentMemorySettingsParams
}

func (f *fakeQuerier) GetUserAgentMemorySettings(_ context.Context, _ cerebrodb.GetUserAgentMemorySettingsParams) (cerebrodb.GetUserAgentMemorySettingsRow, error) {
	return f.getRow, f.getErr
}

func (f *fakeQuerier) UpsertUserAgentMemorySettings(_ context.Context, arg cerebrodb.UpsertUserAgentMemorySettingsParams) (cerebrodb.UpsertUserAgentMemorySettingsRow, error) {
	f.upserted = true
	f.upsertArgs = arg
	return f.upsertRow, nil
}

func (f *fakeQuerier) DeleteUserAgentMemorySettings(_ context.Context, _ cerebrodb.DeleteUserAgentMemorySettingsParams) error {
	f.deleted = true
	return nil
}

var (
	testUser  = util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	testAgent = util.MustParseUUID("22222222-2222-2222-2222-222222222222")
	testWs    = util.MustParseUUID("33333333-3333-3333-3333-333333333333")
)

// A missing row is the default-off case: read and write both false, no error.
func TestGetSettingsDefaultsOffWhenNoRow(t *testing.T) {
	svc := New(&fakeQuerier{getErr: pgx.ErrNoRows}, nil)
	got, err := svc.GetSettings(context.Background(), testUser, testAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CanReadMemory || got.CanWriteMemory {
		t.Fatalf("expected default-off settings, got %+v", got)
	}
}

func TestGetSettingsReturnsStoredRow(t *testing.T) {
	svc := New(&fakeQuerier{getRow: cerebrodb.GetUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: false}}, nil)
	got, err := svc.GetSettings(context.Background(), testUser, testAgent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.CanReadMemory || got.CanWriteMemory {
		t.Fatalf("expected read-on/write-off, got %+v", got)
	}
}

// Turning both switches off deletes the row rather than storing all-false, so
// "default off" stays represented by absence.
func TestSetSettingsBothOffDeletesRow(t *testing.T) {
	fake := &fakeQuerier{}
	svc := New(fake, nil)
	got, err := svc.SetSettings(context.Background(), testWs, testUser, testAgent, Settings{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.deleted {
		t.Fatal("expected delete when both switches off")
	}
	if fake.upserted {
		t.Fatal("did not expect an upsert when both switches off")
	}
	if got.CanReadMemory || got.CanWriteMemory {
		t.Fatalf("expected zero settings, got %+v", got)
	}
}

func TestSetSettingsUpsertsWhenAnySwitchOn(t *testing.T) {
	fake := &fakeQuerier{upsertRow: cerebrodb.UpsertUserAgentMemorySettingsRow{CanReadMemory: true, CanWriteMemory: true}}
	svc := New(fake, nil)
	got, err := svc.SetSettings(context.Background(), testWs, testUser, testAgent, Settings{CanReadMemory: true, CanWriteMemory: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.upserted {
		t.Fatal("expected an upsert when a switch is on")
	}
	if fake.deleted {
		t.Fatal("did not expect a delete when a switch is on")
	}
	if !got.CanReadMemory || !got.CanWriteMemory {
		t.Fatalf("expected both switches on, got %+v", got)
	}
}
