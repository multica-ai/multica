package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChannelService is the entry point for channel and membership operations.
// It deliberately holds no platform integrations — handlers are responsible
// for HTTP serialization, authentication, WebSocket publishing, and task
// enqueuement. See the channels feature spec, section 4.
type ChannelService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// NewChannelService constructs a ChannelService. TxStarter may be nil if the
// caller has no plans to use GetOrCreateDM or any other tx-using method;
// methods that require a transaction return ErrInvalid in that case.
func NewChannelService(q *db.Queries, tx TxStarter) *ChannelService {
	return &ChannelService{Queries: q, TxStarter: tx}
}

// validateKind / validateVisibility are tight allow-lists. Storing them as
// open strings in the schema makes future kinds easy; the service enforces
// the current set so handlers can rely on a known shape.
func validateKind(kind string) error {
	switch kind {
	case KindChannel, KindDM:
		return nil
	default:
		return fmt.Errorf("%w: kind must be %q or %q", ErrInvalid, KindChannel, KindDM)
	}
}

func validateVisibility(v string) error {
	switch v {
	case VisibilityPublic, VisibilityPrivate:
		return nil
	default:
		return fmt.Errorf("%w: visibility must be %q or %q", ErrInvalid, VisibilityPublic, VisibilityPrivate)
	}
}

func validateActorType(t string) error {
	switch t {
	case ActorMember, ActorAgent:
		return nil
	default:
		return fmt.Errorf("%w: actor type must be %q or %q", ErrInvalid, ActorMember, ActorAgent)
	}
}

// validateChannelName enforces the upstream-PR invariants for human-readable
// channel names: lowercase, no spaces, 1-80 chars. DMs use the deterministic
// hash from DMName(...) and bypass this check.
func validateChannelName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalid)
	}
	if len(name) > 80 {
		return fmt.Errorf("%w: name exceeds 80 chars", ErrInvalid)
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("%w: name must be lowercase", ErrInvalid)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("%w: name must not contain whitespace", ErrInvalid)
	}
	return nil
}

// DMName produces the deterministic name used in channel.name for a DM
// between the given actors. The name is a hash so it survives changes to
// display names and never collides with human-readable channel names. The
// result is stable under participant order (the inputs are sorted before
// hashing) and bounded in length.
//
// We include the workspace ID in the input so a participant pair across two
// workspaces never accidentally collides — though the (workspace_id, kind,
// name) unique constraint also guards against this.
func DMName(workspaceID pgtype.UUID, participants []Actor) string {
	parts := make([]string, 0, len(participants))
	for _, p := range participants {
		parts = append(parts, p.Type+":"+uuidString(p.ID))
	}
	sort.Strings(parts)
	h := sha256.New()
	h.Write(workspaceID.Bytes[:])
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(parts, ",")))
	return "dm-" + hex.EncodeToString(h.Sum(nil)[:16]) // 32 hex chars + prefix
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// translateNotFound maps pgx.ErrNoRows to our typed ErrNotFound. Other errors
// pass through unchanged.
func translateNotFound(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Create persists a new channel and adds the creator as the first member with
// admin role. For DMs callers should prefer GetOrCreateDM; Create accepts
// kind=KindDM but does not add the second participant — it's the caller's
// problem in that case.
func (s *ChannelService) Create(ctx context.Context, p CreateChannelParams) (db.Channel, error) {
	if err := validateKind(p.Kind); err != nil {
		return db.Channel{}, err
	}
	if err := validateVisibility(p.Visibility); err != nil {
		return db.Channel{}, err
	}
	if err := validateActorType(p.CreatedBy.Type); err != nil {
		return db.Channel{}, err
	}
	if p.Kind == KindChannel {
		if err := validateChannelName(p.Name); err != nil {
			return db.Channel{}, err
		}
	}
	if p.Kind == KindDM && p.Visibility != VisibilityPrivate {
		return db.Channel{}, fmt.Errorf("%w: DMs must be private", ErrInvalid)
	}

	args := db.CreateChannelParams{
		WorkspaceID:   p.WorkspaceID,
		Name:          p.Name,
		DisplayName:   p.DisplayName,
		Description:   p.Description,
		Kind:          p.Kind,
		Visibility:    p.Visibility,
		CreatedByType: p.CreatedBy.Type,
		CreatedByID:   p.CreatedBy.ID,
	}
	if p.RetentionDays != nil {
		args.RetentionDays = pgtype.Int4{Int32: *p.RetentionDays, Valid: true}
	}
	if len(p.Metadata) > 0 {
		args.Metadata = p.Metadata
	}

	ch, err := s.Queries.CreateChannel(ctx, args)
	if err != nil {
		// Postgres unique violation on (workspace_id, kind, name) → conflict.
		if strings.Contains(err.Error(), "channel_workspace_id_kind_name_key") {
			return db.Channel{}, ErrConflict
		}
		return db.Channel{}, err
	}

	// Add the creator as an admin member. Self-join, so added_by is nil.
	if _, err := s.Queries.AddChannelMember(ctx, db.AddChannelMemberParams{
		ChannelID:         ch.ID,
		MemberType:        p.CreatedBy.Type,
		MemberID:          p.CreatedBy.ID,
		Role:              pgtype.Text{String: RoleAdmin, Valid: true},
		AddedByType:       pgtype.Text{},
		AddedByID:         pgtype.UUID{},
		NotificationLevel: pgtype.Text{String: NotifyAll, Valid: true},
	}); err != nil {
		// Best effort: if creator membership fails, the channel exists but
		// without its creator as member. Higher layers can self-heal by
		// re-adding. We surface the error so callers know.
		return ch, fmt.Errorf("create channel: add creator membership: %w", err)
	}
	return ch, nil
}

// Get returns a channel by id. Workspace scoping is the caller's job — call
// GetInWorkspace if you need it.
func (s *ChannelService) Get(ctx context.Context, id pgtype.UUID) (db.Channel, error) {
	ch, err := s.Queries.GetChannel(ctx, id)
	return ch, translateNotFound(err)
}

// GetInWorkspace returns a channel only if it belongs to the given workspace.
// Use this from any handler that takes a workspace from the request context.
func (s *ChannelService) GetInWorkspace(ctx context.Context, id, workspaceID pgtype.UUID) (db.Channel, error) {
	ch, err := s.Queries.GetChannelInWorkspace(ctx, db.GetChannelInWorkspaceParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
	return ch, translateNotFound(err)
}

// ListForActor returns active channels visible to the given actor. Visibility
// is enforced in SQL so a private channel never appears in the list of an
// actor who isn't a member.
func (s *ChannelService) ListForActor(ctx context.Context, workspaceID pgtype.UUID, actor Actor) ([]db.Channel, error) {
	if err := validateActorType(actor.Type); err != nil {
		return nil, err
	}
	return s.Queries.ListChannelsForActor(ctx, db.ListChannelsForActorParams{
		WorkspaceID: workspaceID,
		MemberType:  actor.Type,
		MemberID:    actor.ID,
	})
}

// Update mutates the in-place fields of a channel. Pass nil for any field
// that should not be touched. RetentionDaysSet=true clears the override when
// RetentionDays is also nil.
func (s *ChannelService) Update(ctx context.Context, id pgtype.UUID, p UpdateChannelParams) (db.Channel, error) {
	args := db.UpdateChannelParams{
		ID:               id,
		RetentionDaysSet: p.RetentionDaysSet,
	}
	if p.DisplayName != nil {
		args.DisplayName = pgtype.Text{String: *p.DisplayName, Valid: true}
	}
	if p.Description != nil {
		args.Description = pgtype.Text{String: *p.Description, Valid: true}
	}
	if p.Visibility != nil {
		if err := validateVisibility(*p.Visibility); err != nil {
			return db.Channel{}, err
		}
		args.Visibility = pgtype.Text{String: *p.Visibility, Valid: true}
	}
	if p.RetentionDays != nil {
		args.RetentionDays = pgtype.Int4{Int32: *p.RetentionDays, Valid: true}
	}
	if len(p.Metadata) > 0 {
		args.Metadata = p.Metadata
	}
	ch, err := s.Queries.UpdateChannel(ctx, args)
	return ch, translateNotFound(err)
}

// Archive marks a channel archived. Messages are retained until the retention
// sweep collects them; subsequent listings exclude archived rows.
func (s *ChannelService) Archive(ctx context.Context, id pgtype.UUID) error {
	return s.Queries.ArchiveChannel(ctx, id)
}

// AddMember inserts a membership row. Adding an existing member is idempotent
// (returns the existing row).
func (s *ChannelService) AddMember(ctx context.Context, channelID pgtype.UUID, p AddMemberParams) (db.ChannelMembership, error) {
	if err := validateActorType(p.Member.Type); err != nil {
		return db.ChannelMembership{}, err
	}

	args := db.AddChannelMemberParams{
		ChannelID:  channelID,
		MemberType: p.Member.Type,
		MemberID:   p.Member.ID,
	}
	if p.Role != "" {
		args.Role = pgtype.Text{String: p.Role, Valid: true}
	}
	if p.AddedBy != nil {
		if err := validateActorType(p.AddedBy.Type); err != nil {
			return db.ChannelMembership{}, err
		}
		args.AddedByType = pgtype.Text{String: p.AddedBy.Type, Valid: true}
		args.AddedByID = p.AddedBy.ID
	}
	if p.NotificationLevel != "" {
		args.NotificationLevel = pgtype.Text{String: p.NotificationLevel, Valid: true}
	}

	mem, err := s.Queries.AddChannelMember(ctx, args)
	if err == nil {
		return mem, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelMembership{}, err
	}
	// ON CONFLICT DO NOTHING returns no row when the member already exists.
	// Fetch the existing row so the caller gets a uniform return.
	existing, err := s.Queries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  channelID,
		MemberType: p.Member.Type,
		MemberID:   p.Member.ID,
	})
	return existing, translateNotFound(err)
}

// RemoveMember deletes a membership. No-op if the membership did not exist.
func (s *ChannelService) RemoveMember(ctx context.Context, channelID pgtype.UUID, member Actor) error {
	if err := validateActorType(member.Type); err != nil {
		return err
	}
	return s.Queries.RemoveChannelMember(ctx, db.RemoveChannelMemberParams{
		ChannelID:  channelID,
		MemberType: member.Type,
		MemberID:   member.ID,
	})
}

// ListMembers returns all members of a channel ordered by join time.
func (s *ChannelService) ListMembers(ctx context.Context, channelID pgtype.UUID) ([]db.ChannelMembership, error) {
	return s.Queries.ListChannelMembers(ctx, channelID)
}

// MarkRead advances an actor's read cursor on a channel. Out-of-order
// delivery is tolerated: the cursor is "the last message I saw", not a count.
func (s *ChannelService) MarkRead(ctx context.Context, channelID pgtype.UUID, actor Actor, upToMessageID pgtype.UUID) error {
	if err := validateActorType(actor.Type); err != nil {
		return err
	}
	return s.Queries.MarkChannelRead(ctx, db.MarkChannelReadParams{
		ChannelID:         channelID,
		MemberType:        actor.Type,
		MemberID:          actor.ID,
		LastReadMessageID: upToMessageID,
	})
}

// CanActorAccess reports whether an actor may read a channel. The rule:
//   - the channel exists and belongs to workspaceID
//   - it is not archived
//   - either the channel is a public 'channel', or the actor is a member
//
// Returns (false, ErrNotFound) when the channel doesn't exist in the
// workspace, so callers can serve a 404 without an extra lookup.
func (s *ChannelService) CanActorAccess(ctx context.Context, channelID, workspaceID pgtype.UUID, actor Actor) (bool, error) {
	ch, err := s.GetInWorkspace(ctx, channelID, workspaceID)
	if err != nil {
		return false, err
	}
	if ch.ArchivedAt.Valid {
		return false, ErrChannelClosed
	}
	if ch.Kind == KindChannel && ch.Visibility == VisibilityPublic {
		return true, nil
	}
	_, err = s.Queries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  channelID,
		MemberType: actor.Type,
		MemberID:   actor.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetOrCreateDM returns an existing DM between the given participants or
// creates a new one. Idempotent: callers can invoke this on every "open DM"
// click without duplicating rows. Participants must be 1+ actors; the
// service deduplicates them by (type, id) so [a, a, b] becomes [a, b].
//
// Each participant is added as a regular member (not admin). The first actor
// in the deduplicated list is considered the creator for created_by_*.
func (s *ChannelService) GetOrCreateDM(ctx context.Context, workspaceID pgtype.UUID, participants []Actor) (db.Channel, error) {
	if len(participants) == 0 {
		return db.Channel{}, fmt.Errorf("%w: at least one participant required", ErrInvalid)
	}
	deduped := dedupeActors(participants)
	for _, a := range deduped {
		if err := validateActorType(a.Type); err != nil {
			return db.Channel{}, err
		}
	}

	name := DMName(workspaceID, deduped)

	// Fast path: row already exists.
	if existing, err := s.Queries.GetChannelByName(ctx, db.GetChannelByNameParams{
		WorkspaceID: workspaceID,
		Kind:        KindDM,
		Name:        name,
	}); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.Channel{}, err
	}

	if s.TxStarter == nil {
		return db.Channel{}, fmt.Errorf("%w: GetOrCreateDM requires TxStarter", ErrInvalid)
	}

	// Slow path: create channel + memberships in one transaction. Concurrent
	// callers may race; the loser's INSERT will violate the unique constraint
	// on (workspace_id, kind, name). We retry the read and return that row.
	var ch db.Channel
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Channel{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // safe even after Commit

	q := s.Queries.WithTx(tx)
	ch, err = q.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID:   workspaceID,
		Name:          name,
		DisplayName:   "", // UI builds from membership
		Description:   "",
		Kind:          KindDM,
		Visibility:    VisibilityPrivate,
		CreatedByType: deduped[0].Type,
		CreatedByID:   deduped[0].ID,
	})
	if err != nil {
		// Concurrent winner created the row first.
		if strings.Contains(err.Error(), "channel_workspace_id_kind_name_key") {
			_ = tx.Rollback(ctx)
			return s.Queries.GetChannelByName(ctx, db.GetChannelByNameParams{
				WorkspaceID: workspaceID,
				Kind:        KindDM,
				Name:        name,
			})
		}
		return db.Channel{}, err
	}
	for _, a := range deduped {
		if _, err := q.AddChannelMember(ctx, db.AddChannelMemberParams{
			ChannelID:         ch.ID,
			MemberType:        a.Type,
			MemberID:          a.ID,
			Role:              pgtype.Text{String: RoleMember, Valid: true},
			NotificationLevel: pgtype.Text{String: NotifyAll, Valid: true},
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return db.Channel{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Channel{}, err
	}
	return ch, nil
}

// dedupeActors removes duplicates while preserving the order of first
// occurrence. Used by GetOrCreateDM so a buggy caller that passes the same
// actor twice doesn't end up with a degenerate DM.
func dedupeActors(actors []Actor) []Actor {
	seen := make(map[string]struct{}, len(actors))
	out := make([]Actor, 0, len(actors))
	for _, a := range actors {
		key := a.Type + ":" + uuidString(a.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a)
	}
	return out
}
