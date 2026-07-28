package note

import (
	"context"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestCollabAccessLiveEnabledPrecedence(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "Live flag", "body")
	access := CollabAccess{Cerebro: w3H.Cerebro}
	note := uuidStr(noteID)
	user := uuidStr(w3UserA)

	enabled, err := access.LiveEnabled(ctx, note, user)
	if err != nil {
		t.Fatalf("default live flag: %v", err)
	}
	if enabled {
		t.Fatal("live editing defaulted on, want off")
	}

	if err := w3H.Cerebro.UpsertCerebroFeatureFlag(ctx, cerebrodb.UpsertCerebroFeatureFlagParams{
		WorkspaceID: w3WsID,
		UserID:      w3UserA,
		FlagKey:     liveCollabFlag,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("enable personal live flag: %v", err)
	}
	enabled, err = access.LiveEnabled(ctx, note, user)
	if err != nil || !enabled {
		t.Fatalf("personal live flag = %v, %v; want true, nil", enabled, err)
	}

	if err := w3H.Cerebro.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: w3WsID,
		FlagKey:     liveCollabFlag,
		Enabled:     false,
		Locked:      true,
	}); err != nil {
		t.Fatalf("lock workspace live flag off: %v", err)
	}
	enabled, err = access.LiveEnabled(ctx, note, user)
	if err != nil {
		t.Fatalf("locked workspace live flag: %v", err)
	}
	if enabled {
		t.Fatal("personal override bypassed locked workspace off flag")
	}
}
