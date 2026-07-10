// Artifact version history (FIR-2697). Version history used to exist only for
// personal Notes (edited through /api/notes, snapshotted inline by UpdateNote).
// Agent-created documents are plain artifacts edited through /api/artifacts (the
// Documents editor, the CLI `artifact`/`document` commands, and the MCP
// update_artifact tool) — none of which touched the note handler, so an update
// created a brand-new file's worth of history nowhere.
//
// This listener closes that gap without touching the upstream artifact handler:
// that handler already publishes artifact:created / artifact:updated on the
// event bus, so we subscribe and snapshot a version from the event. The version
// rows land in the same cerebro_note_version table (its FK was widened to
// artifact(id) in migration 9120), so notes and documents share one engine.
//
// The subscription is registered once at server start
// (RegisterArtifactVersionListener in cmd/server/main.go).
package note

import (
	"context"
	"encoding/json"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// These string literals are the wire contract published by the upstream artifact
// handler (server/internal/handler/artifact.go). They are intentionally NOT
// imported from that package (it imports this one — importing back would cycle),
// and are deliberately kept out of pkg/protocol to stay merge-additive, matching
// where the handler defines them.
const (
	eventArtifactCreated = "artifact:created"
	eventArtifactUpdated = "artifact:updated"
)

// artifactVersioner snapshots a version whenever an agent-created document is
// saved. It holds only the cerebro Queries — the snapshot core
// (writeVersionSnapshot) needs nothing else, so no HTTP Handler is required.
type artifactVersioner struct {
	cerebro *cerebrodb.Queries
}

// eventArtifact is the slice of the artifact:created/updated payload we need. It
// mirrors the JSON the upstream handler publishes (map[string]any{"artifact":
// artifactToResponse(...)}); decoding via JSON keeps us decoupled from that
// handler's Go response type.
type eventArtifact struct {
	Artifact struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"artifact"`
}

// RegisterArtifactVersionListener wires artifact save events to version snapshots.
// Call once at server start, after the event bus exists.
func RegisterArtifactVersionListener(bus *events.Bus, cerebro *cerebrodb.Queries) {
	av := &artifactVersioner{cerebro: cerebro}
	bus.Subscribe(eventArtifactCreated, av.onArtifactSaved)
	bus.Subscribe(eventArtifactUpdated, av.onArtifactSaved)
}

// onArtifactSaved records the saved artifact's title+body as a version. The
// author comes from the event actor (an agent for agent-authored documents, a
// member for human edits), so the history shows who changed what. Best-effort:
// any failure is swallowed — the live artifact content is already persisted, the
// history is a side record.
func (av *artifactVersioner) onArtifactSaved(e events.Event) {
	var payload eventArtifact
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	a := payload.Artifact
	if a.ID == "" {
		return
	}
	// Only agents and members author versions; a 'system' actor has no identity
	// to attribute a version to, so skip it.
	if e.ActorType != "agent" && e.ActorType != "member" {
		return
	}
	artifactID, err := util.ParseUUID(a.ID)
	if err != nil {
		return
	}
	authorID, err := util.ParseUUID(e.ActorID)
	if err != nil {
		return
	}

	ctx := context.Background()

	// Skip a no-op save: if the latest recorded version already matches this
	// title+body, adding another row would be noise. (Coalescing in
	// writeVersionSnapshot only collapses same-author bursts; this also covers a
	// different author re-saving identical content, e.g. a scope/folder-only
	// update that re-emits artifact:updated.)
	if latest, err := av.cerebro.GetLatestNoteVersion(ctx, artifactID); err == nil &&
		latest.Title == a.Title && latest.Body == a.Body {
		return
	}

	writeVersionSnapshot(ctx, av.cerebro, artifactID, authorID, e.ActorType, a.Title, a.Body, "edit", "", true)
}
