// Phase 7d Ship Hub — release finalizer.
//
// Every 15 minutes, walk in_production releases whose promoted_at is
// older than the monitoring window (24h by default) AND have not been
// rolled back. Transition each to stage='done', stamp done_at, emit
// the WS event + an audit row.
//
// Why a separate goroutine instead of folding it into the per-deploy
// health monitor: the cadence is different (5min monitor vs 15min
// finalizer) and the read pattern is different (fresh per-deploy
// snapshots vs an age-based filter on releases). Keeping them as
// separate ticks is cleaner — and the finalizer's tick is so cheap
// (a single query, a handful of UPDATE statements per pass) that
// piggybacking on the monitor's loop wouldn't save anything.
//
// Single-node only at this stage. The finalizer's UpdateReleaseStage
// call is idempotent on stage='done' (the next tick's
// ListReleasesPastMonitoringWindow excludes done releases by stage),
// so a duplicate write from a multi-node deploy is harmless.

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// shipHubReleaseFinalizerInterval — every 15 minutes. Releases past
// the 24h window are batch-eligible; running this every minute would
// just burn DB cycles for no UX benefit (a release closing 14 minutes
// late is fine).
const shipHubReleaseFinalizerInterval = 15 * time.Minute

// shipHubReleaseMonitoringWindow — the 24h post-promote watch the
// rollback affordance is keyed on. After this elapses without a
// rollback the release auto-closes to stage=done.
const shipHubReleaseMonitoringWindow = 24 * time.Hour

// shipHubBusPublisher is the slim slice of the events bus the
// finalizer needs. Defining it as an interface (rather than passing
// *events.Bus directly) lets a future test pass nil and skip WS
// publication without faking the entire bus.
type shipHubBusPublisher interface {
	Publish(eventType, workspaceID string, payload map[string]any)
}

// finalizerBusAdapter wraps an *events.Bus to expose the
// shipHubBusPublisher shape. Cheap pass-through.
type finalizerBusAdapter struct{ bus *events.Bus }

func (a *finalizerBusAdapter) Publish(eventType, workspaceID string, payload map[string]any) {
	if a == nil || a.bus == nil {
		return
	}
	a.bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

func runShipHubReleaseFinalizer(
	ctx context.Context,
	queries *db.Queries,
	bus shipHubBusPublisher,
) {
	slog.Info("ship hub release finalizer started",
		"interval", shipHubReleaseFinalizerInterval.String(),
		"window", shipHubReleaseMonitoringWindow.String())
	t := time.NewTicker(shipHubReleaseFinalizerInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("ship hub release finalizer stopped")
			return
		case <-t.C:
			runShipHubReleaseFinalizerOnce(ctx, queries, bus)
		}
	}
}

// runShipHubReleaseFinalizerOnce — one pass. Extracted so a future
// test can drive it deterministically.
func runShipHubReleaseFinalizerOnce(
	ctx context.Context,
	queries *db.Queries,
	bus shipHubBusPublisher,
) {
	cutoff := time.Now().Add(-shipHubReleaseMonitoringWindow)
	releases, err := queries.ListReleasesPastMonitoringWindow(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		slog.Warn("ship hub release finalizer: list past-window failed", "error", err)
		return
	}
	for _, rel := range releases {
		now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
		updated, err := queries.UpdateReleaseStage(ctx, db.UpdateReleaseStageParams{
			ID:     rel.ID,
			Stage:  db.ReleaseStageDone,
			DoneAt: now,
		})
		if err != nil {
			slog.Warn("ship hub release finalizer: update stage failed",
				"release_id", util.UUIDToString(rel.ID), "error", err)
			continue
		}
		// Audit row.
		payload, _ := json.Marshal(map[string]any{
			"reason": "24h post-deploy window elapsed without rollback",
		})
		_, _ = queries.InsertReleaseEvent(ctx, db.InsertReleaseEventParams{
			ReleaseID:   rel.ID,
			EventType:   "release_done",
			ActorUserID: pgtype.UUID{},
			Payload:     payload,
		})
		// WS event so the release page flips without a refresh.
		if bus != nil {
			bus.Publish(protocol.EventReleaseUpdated, util.UUIDToString(updated.WorkspaceID), map[string]any{
				"release_id": util.UUIDToString(updated.ID),
				"stage":      string(updated.Stage),
			})
		}
	}
}
