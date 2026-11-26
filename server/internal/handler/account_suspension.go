package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Account suspension (MUL-10296) is an operator action: it is served only on
// the maintenance listener, never on the public router. Suspending blocks
// every credential the account holds and keeps its memberships and data, so
// unsuspending restores the account as it was. An incident response also
// revokes the account's credentials and stops what it runs in every
// workspace it belongs to.

var (
	errAccountRequest    = errors.New("invalid request")
	errAccountNotFound   = errors.New("user not found")
	errAccountIncidentOn = errors.New("an incident response for this account has not finished; rerun it before unsuspending")
)

// AccountActionRequest names the account by user_id or email, and records who
// acted and why.
type AccountActionRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	// DryRun applies only to incident response: every change runs and is
	// rolled back, and the report shows what a real run would do.
	DryRun bool `json:"dry_run"`
}

// AccountReport is the response to every account action.
type AccountReport struct {
	User      AccountUser    `json:"user"`
	Event     *AccountEvent  `json:"event,omitempty"`
	Events    []AccountEvent `json:"events,omitempty"`
	DryRun    bool           `json:"dry_run,omitempty"`
	DryReport map[string]any `json:"dry_run_report,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
}

type AccountUser struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	SuspendedAt     *time.Time `json:"suspended_at"`
	SuspendedReason string     `json:"suspended_reason,omitempty"`
	SuspendedBy     string     `json:"suspended_by,omitempty"`
	TokenVersion    int32      `json:"token_version"`
}

type AccountEvent struct {
	ID          string          `json:"id"`
	Action      string          `json:"action"`
	Actor       string          `json:"actor"`
	Reason      string          `json:"reason"`
	Status      string          `json:"status"`
	Report      json.RawMessage `json:"report"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// incidentCredentials is report.credentials of an incident response.
type incidentCredentials struct {
	SessionsEndedAtVersion      int32 `json:"sessions_ended_at_version"`
	PersonalAccessTokensRevoked int   `json:"personal_access_tokens_revoked"`
	DaemonTokensDeleted         int   `json:"daemon_tokens_deleted"`
}

// incidentWorkspace is report.workspaces[<id>] of an incident response.
type incidentWorkspace struct {
	Runtimes                 []incidentRef `json:"runtimes_offline"`
	AgentsArchived           []incidentRef `json:"agents_archived"`
	ProductAgentsTransferred []incidentRef `json:"product_agents_transferred"`
	AutopilotsPaused         []incidentRef `json:"autopilots_paused"`
	TasksCancelled           int           `json:"tasks_cancelled"`
	DaemonTokensDeleted      int           `json:"daemon_tokens_deleted"`
	IssueWakeupsDisabled     int64         `json:"issue_wakeups_disabled"`
	ChatWakeupsEnded         int64         `json:"chat_wakeups_ended"`
	// NoOtherOwner: the account is this workspace's only owner. Its product
	// agents stay with it, and nobody else can manage the workspace.
	NoOtherOwner bool `json:"no_other_owner,omitempty"`
}

type incidentRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DaemonID string `json:"daemon_id,omitempty"`
}

// cloudTokenWarning: mcn_ tokens are issued by Multica Cloud. Suspension
// refuses them, but only Cloud can revoke them.
const cloudTokenWarning = "Cloud node tokens (mcn_) for this account are refused while it is suspended but can only be revoked in Multica Cloud."

func (h *Handler) resolveAccount(ctx context.Context, req AccountActionRequest) (db.User, error) {
	userID := strings.TrimSpace(req.UserID)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if (userID == "") == (email == "") {
		return db.User{}, fmt.Errorf("%w: give exactly one of user_id or email", errAccountRequest)
	}
	var user db.User
	var err error
	if userID != "" {
		id, perr := util.ParseUUID(userID)
		if perr != nil {
			return db.User{}, fmt.Errorf("%w: user_id is not a UUID", errAccountRequest)
		}
		user, err = h.Queries.GetUser(ctx, id)
	} else {
		user, err = h.Queries.GetUserByEmail(ctx, email)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, errAccountNotFound
	}
	return user, err
}

func requireActorAndReason(req AccountActionRequest) error {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("%w: actor and reason are required", errAccountRequest)
	}
	return nil
}

func rejectDryRun(req AccountActionRequest) error {
	if req.DryRun {
		return fmt.Errorf("%w: dry_run applies only to incident-response", errAccountRequest)
	}
	return requireActorAndReason(req)
}

// SuspendAccount blocks every credential the account holds. Suspending a
// suspended account keeps the first suspension's time and reason.
func (h *Handler) SuspendAccount(ctx context.Context, req AccountActionRequest) (AccountReport, error) {
	if err := rejectDryRun(req); err != nil {
		return AccountReport{}, err
	}
	user, err := h.resolveAccount(ctx, req)
	if err != nil {
		return AccountReport{}, err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return AccountReport{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if user, err = qtx.GetUserForSuspension(ctx, user.ID); err != nil {
		return AccountReport{}, err
	}
	if !user.SuspendedAt.Valid {
		if user, err = qtx.SuspendUser(ctx, db.SuspendUserParams{
			ID:          user.ID,
			Reason:      pgtype.Text{String: req.Reason, Valid: true},
			SuspendedBy: pgtype.Text{String: req.Actor, Valid: true},
		}); err != nil {
			return AccountReport{}, err
		}
	}
	event, err := qtx.CreateUserSuspensionEvent(ctx, db.CreateUserSuspensionEventParams{
		UserID: user.ID, Action: "suspend", Actor: req.Actor, Reason: req.Reason,
		Status: "completed", Report: []byte(`{}`),
	})
	if err != nil {
		return AccountReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountReport{}, err
	}
	h.AccountStates.Put(ctx, uuidToString(user.ID), auth.AccountState{Suspended: true, TokenVersion: user.TokenVersion})
	slog.Warn("account suspended", "user_id", uuidToString(user.ID), "actor", req.Actor, "event_id", uuidToString(event.ID))
	ev := accountEvent(event)
	return AccountReport{
		User:     accountUser(user),
		Event:    &ev,
		Warnings: []string{"Credentials were not revoked: they work again if the account is unsuspended. Run an incident response to revoke them.", cloudTokenWarning},
	}, nil
}

// IncidentResponse suspends the account, revokes its credentials and stops
// what it runs in each of its workspaces, recording a report. Each workspace
// commits on its own together with its entry in the report, so a run that
// fails or times out part-way is finished by running it again.
func (h *Handler) IncidentResponse(ctx context.Context, req AccountActionRequest) (AccountReport, error) {
	if err := requireActorAndReason(req); err != nil {
		return AccountReport{}, err
	}
	user, err := h.resolveAccount(ctx, req)
	if err != nil {
		return AccountReport{}, err
	}
	if req.DryRun {
		return h.incidentDryRun(ctx, user)
	}

	user, event, creds, err := h.beginIncident(ctx, user, req)
	if err != nil {
		return AccountReport{}, err
	}
	userIDStr := uuidToString(user.ID)
	h.AccountStates.Put(ctx, userIDStr, auth.AccountState{Suspended: true, TokenVersion: user.TokenVersion})
	for _, hash := range creds.patHashes {
		h.PATCache.Invalidate(ctx, hash)
	}
	for _, hash := range creds.daemonHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}

	done, err := incidentWorkspacesDone(event.Report)
	if err != nil {
		return AccountReport{}, err
	}
	workspaceIDs, err := h.Queries.ListWorkspaceIDsByUser(ctx, user.ID)
	if err != nil {
		return AccountReport{}, err
	}
	for _, wsID := range workspaceIDs {
		if done[uuidToString(wsID)] {
			continue
		}
		if err := h.stopAccountInWorkspace(ctx, event.ID, wsID, user.ID, false, nil); err != nil {
			return AccountReport{}, fmt.Errorf("workspace %s: %w", uuidToString(wsID), err)
		}
	}

	event, err = h.Queries.CompleteUserSuspensionEvent(ctx, db.CompleteUserSuspensionEventParams{
		ID: event.ID, Summary: []byte(`{}`),
	})
	if err != nil {
		return AccountReport{}, err
	}
	h.notifyDaemonWorkspacesChanged(userIDStr)
	slog.Warn("account incident response completed", "user_id", userIDStr, "actor", req.Actor, "event_id", uuidToString(event.ID), "workspaces", len(workspaceIDs))
	ev := accountEvent(event)
	return AccountReport{User: accountUser(user), Event: &ev, Warnings: []string{cloudTokenWarning}}, nil
}

type revokedCredentials struct {
	patHashes    []string
	daemonHashes []string
}

// beginIncident suspends the account and, unless an earlier run is being
// resumed, ends its sessions, revokes its tokens and opens the report, all in
// one transaction.
func (h *Handler) beginIncident(ctx context.Context, user db.User, req AccountActionRequest) (db.User, db.UserSuspensionEvent, revokedCredentials, error) {
	var creds revokedCredentials
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if user, err = qtx.GetUserForSuspension(ctx, user.ID); err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	if !user.SuspendedAt.Valid {
		if user, err = qtx.SuspendUser(ctx, db.SuspendUserParams{
			ID:          user.ID,
			Reason:      pgtype.Text{String: req.Reason, Valid: true},
			SuspendedBy: pgtype.Text{String: req.Actor, Valid: true},
		}); err != nil {
			return user, db.UserSuspensionEvent{}, creds, err
		}
	}
	event, err := qtx.GetRunningIncidentResponse(ctx, user.ID)
	if err == nil {
		// Resuming: the credentials were revoked by the run that opened it.
		if err := tx.Commit(ctx); err != nil {
			return user, db.UserSuspensionEvent{}, creds, err
		}
		return user, event, creds, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	version, revoked, err := revokeAccountCredentials(ctx, qtx, user.ID)
	if err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	user.TokenVersion = version
	creds = revoked
	report, err := json.Marshal(map[string]any{
		"credentials": incidentCredentials{
			SessionsEndedAtVersion:      version,
			PersonalAccessTokensRevoked: len(revoked.patHashes),
			DaemonTokensDeleted:         len(revoked.daemonHashes),
		},
		"workspaces": map[string]any{},
	})
	if err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	event, err = qtx.CreateUserSuspensionEvent(ctx, db.CreateUserSuspensionEventParams{
		UserID: user.ID, Action: "incident_response", Actor: req.Actor, Reason: req.Reason,
		Status: "running", Report: report,
	})
	if err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	if err := tx.Commit(ctx); err != nil {
		return user, db.UserSuspensionEvent{}, creds, err
	}
	return user, event, creds, nil
}

// revokeAccountCredentials ends every session, revokes every personal access
// token and deletes every daemon token of the account.
func revokeAccountCredentials(ctx context.Context, qtx *db.Queries, userID pgtype.UUID) (int32, revokedCredentials, error) {
	var creds revokedCredentials
	version, err := qtx.EndUserSessions(ctx, userID)
	if err != nil {
		return 0, creds, err
	}
	if creds.patHashes, err = qtx.RevokePersonalAccessTokensByUser(ctx, userID); err != nil {
		return 0, creds, err
	}
	if creds.daemonHashes, err = qtx.DeleteDaemonTokensByOwner(ctx, userID); err != nil {
		return 0, creds, err
	}
	return version, creds, nil
}

// stopAccountInWorkspace stops what the account runs in one workspace and
// records it in the incident report, in one transaction. dryRun rolls it back
// and hands the would-be entry to collect instead.
func (h *Handler) stopAccountInWorkspace(ctx context.Context, eventID, workspaceID, userID pgtype.UUID, dryRun bool, collect func(incidentWorkspace)) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if !dryRun {
		// A rerun started while an earlier run is still going waits here and
		// then finds the workspace done, instead of overwriting its entry.
		recorded, err := qtx.LockIncidentWorkspaceRecorded(ctx, db.LockIncidentWorkspaceRecordedParams{
			ID: eventID, WorkspaceID: uuidToString(workspaceID),
		})
		if err != nil {
			return err
		}
		if recorded {
			return nil
		}
	}
	// Same lock order as revokeMember, which can run for this member at the
	// same time.
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{WorkspaceID: workspaceID, UserID: userID}); err != nil {
		return err
	}
	if _, err := qtx.LockWorkspaceOwners(ctx, workspaceID); err != nil {
		return err
	}
	if _, err := qtx.LockRuntimeShareMemberRemoval(ctx, db.LockRuntimeShareMemberRemovalParams{WorkspaceID: workspaceID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
		// Removed from the workspace since the list was read; the removal
		// stopped everything already.
		return nil
	} else if err != nil {
		return err
	}
	result, err := stopMemberAssets(ctx, qtx, workspaceID, userID, pgtype.UUID{})
	if err != nil {
		return err
	}
	entry := incidentWorkspaceReport(result)
	if dryRun {
		collect(entry)
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := qtx.RecordIncidentWorkspace(ctx, db.RecordIncidentWorkspaceParams{
		ID: eventID, WorkspaceID: uuidToString(workspaceID), Result: raw,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	wsIDStr := uuidToString(workspaceID)
	logRevocation(result, wsIDStr, uuidToString(userID), "event_id", uuidToString(eventID))
	h.publishRevocation(ctx, result, wsIDStr, "system", "")
	return nil
}

// incidentDryRun runs every step of an incident response in transactions it
// rolls back, and reports what they changed.
func (h *Handler) incidentDryRun(ctx context.Context, user db.User) (AccountReport, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return AccountReport{}, err
	}
	_, creds, err := revokeAccountCredentials(ctx, h.Queries.WithTx(tx), user.ID)
	_ = tx.Rollback(ctx)
	if err != nil {
		return AccountReport{}, err
	}
	workspaceIDs, err := h.Queries.ListWorkspaceIDsByUser(ctx, user.ID)
	if err != nil {
		return AccountReport{}, err
	}
	workspaces := map[string]incidentWorkspace{}
	for _, wsID := range workspaceIDs {
		key := uuidToString(wsID)
		if err := h.stopAccountInWorkspace(ctx, pgtype.UUID{}, wsID, user.ID, true, func(entry incidentWorkspace) {
			workspaces[key] = entry
		}); err != nil {
			return AccountReport{}, fmt.Errorf("workspace %s: %w", key, err)
		}
	}
	return AccountReport{
		User:   accountUser(user),
		DryRun: true,
		DryReport: map[string]any{
			"credentials": incidentCredentials{
				SessionsEndedAtVersion:      user.TokenVersion + 1,
				PersonalAccessTokensRevoked: len(creds.patHashes),
				DaemonTokensDeleted:         len(creds.daemonHashes),
			},
			"workspaces": workspaces,
		},
		Warnings: []string{cloudTokenWarning},
	}, nil
}

// UnsuspendAccount lets the account sign in again. Credentials an incident
// response revoked stay revoked; after a plain suspension they work again.
func (h *Handler) UnsuspendAccount(ctx context.Context, req AccountActionRequest) (AccountReport, error) {
	if err := rejectDryRun(req); err != nil {
		return AccountReport{}, err
	}
	user, err := h.resolveAccount(ctx, req)
	if err != nil {
		return AccountReport{}, err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return AccountReport{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if user, err = qtx.GetUserForSuspension(ctx, user.ID); err != nil {
		return AccountReport{}, err
	}
	if _, err := qtx.GetRunningIncidentResponse(ctx, user.ID); err == nil {
		return AccountReport{}, errAccountIncidentOn
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return AccountReport{}, err
	}
	var warnings []string
	if user.SuspendedAt.Valid {
		revoked, err := revokedSinceSuspension(ctx, qtx, user)
		if err != nil {
			return AccountReport{}, err
		}
		if !revoked {
			warnings = append(warnings, "No incident response ran since the suspension: the account's existing sessions and tokens work again.")
		}
		if user, err = qtx.UnsuspendUser(ctx, user.ID); err != nil {
			return AccountReport{}, err
		}
	} else {
		warnings = append(warnings, "The account was not suspended.")
	}
	event, err := qtx.CreateUserSuspensionEvent(ctx, db.CreateUserSuspensionEventParams{
		UserID: user.ID, Action: "unsuspend", Actor: req.Actor, Reason: req.Reason,
		Status: "completed", Report: []byte(`{}`),
	})
	if err != nil {
		return AccountReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountReport{}, err
	}
	h.AccountStates.Put(ctx, uuidToString(user.ID), auth.AccountState{Suspended: false, TokenVersion: user.TokenVersion})
	slog.Warn("account unsuspended", "user_id", uuidToString(user.ID), "actor", req.Actor, "event_id", uuidToString(event.ID))
	ev := accountEvent(event)
	return AccountReport{User: accountUser(user), Event: &ev, Warnings: warnings}, nil
}

// revokedSinceSuspension reports whether an incident response ran since the
// account was last suspended.
func revokedSinceSuspension(ctx context.Context, qtx *db.Queries, user db.User) (bool, error) {
	events, err := qtx.ListUserSuspensionEvents(ctx, user.ID)
	if err != nil {
		return false, err
	}
	for _, e := range events {
		if e.Action == "unsuspend" {
			return false, nil
		}
		if e.Action == "incident_response" {
			return true, nil
		}
	}
	return false, nil
}

// AccountStatus returns the account's suspension state and its latest events.
func (h *Handler) AccountStatus(ctx context.Context, req AccountActionRequest) (AccountReport, error) {
	user, err := h.resolveAccount(ctx, req)
	if err != nil {
		return AccountReport{}, err
	}
	events, err := h.Queries.ListUserSuspensionEvents(ctx, user.ID)
	if err != nil {
		return AccountReport{}, err
	}
	out := AccountReport{User: accountUser(user)}
	for _, e := range events {
		out.Events = append(out.Events, accountEvent(e))
	}
	return out, nil
}

func incidentWorkspacesDone(report []byte) (map[string]bool, error) {
	var parsed struct {
		Workspaces map[string]json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal(report, &parsed); err != nil {
		return nil, err
	}
	done := make(map[string]bool, len(parsed.Workspaces))
	for id := range parsed.Workspaces {
		done[id] = true
	}
	return done, nil
}

func incidentWorkspaceReport(result revocationResult) incidentWorkspace {
	entry := incidentWorkspace{
		Runtimes:                 []incidentRef{},
		AgentsArchived:           []incidentRef{},
		ProductAgentsTransferred: []incidentRef{},
		AutopilotsPaused:         []incidentRef{},
		TasksCancelled:           len(result.CancelledTasks) + len(result.Retired.CancelledTasks),
		DaemonTokensDeleted:      len(result.RevokedTokenHashes),
		IssueWakeupsDisabled:     result.Retired.DisabledWakeups,
		ChatWakeupsEnded:         result.Retired.EndedChatWakeups,
		NoOtherOwner:             !result.Heir.Valid,
	}
	for _, rt := range result.Runtimes {
		entry.Runtimes = append(entry.Runtimes, incidentRef{ID: uuidToString(rt.ID), Name: rt.Name, DaemonID: rt.DaemonID.String})
	}
	for _, a := range append(append([]db.Agent{}, result.ArchivedAgents...), result.Retired.ArchivedAgents...) {
		entry.AgentsArchived = append(entry.AgentsArchived, incidentRef{ID: uuidToString(a.ID), Name: a.Name})
	}
	for _, a := range result.Retired.TransferredAgents {
		entry.ProductAgentsTransferred = append(entry.ProductAgentsTransferred, incidentRef{ID: uuidToString(a.ID), Name: a.Name})
	}
	for _, ap := range result.Retired.PausedAutopilots {
		entry.AutopilotsPaused = append(entry.AutopilotsPaused, incidentRef{ID: uuidToString(ap.ID), Name: ap.Title})
	}
	return entry
}

func accountUser(u db.User) AccountUser {
	out := AccountUser{
		ID:              uuidToString(u.ID),
		Email:           u.Email,
		SuspendedReason: u.SuspendedReason.String,
		SuspendedBy:     u.SuspendedBy.String,
		TokenVersion:    u.TokenVersion,
	}
	if u.SuspendedAt.Valid {
		t := u.SuspendedAt.Time
		out.SuspendedAt = &t
	}
	return out
}

func accountEvent(e db.UserSuspensionEvent) AccountEvent {
	out := AccountEvent{
		ID: uuidToString(e.ID), Action: e.Action, Actor: e.Actor, Reason: e.Reason,
		Status: e.Status, Report: json.RawMessage(e.Report), CreatedAt: e.CreatedAt.Time,
	}
	if e.CompletedAt.Valid {
		t := e.CompletedAt.Time
		out.CompletedAt = &t
	}
	return out
}

// AccountAdminHandler serves the account actions under /maintenance/accounts/
// for the maintenance listener. It must never be mounted on the public router.
func (h *Handler) AccountAdminHandler() http.Handler {
	mux := http.NewServeMux()
	route := func(path string, action func(context.Context, AccountActionRequest) (AccountReport, error)) {
		mux.HandleFunc("POST /maintenance/accounts/"+path, func(w http.ResponseWriter, r *http.Request) {
			var req AccountActionRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				writeAccountResult(w, AccountReport{}, fmt.Errorf("%w: %v", errAccountRequest, err))
				return
			}
			report, err := action(r.Context(), req)
			writeAccountResult(w, report, err)
		})
	}
	route("suspend", h.SuspendAccount)
	route("incident-response", h.IncidentResponse)
	route("unsuspend", h.UnsuspendAccount)
	route("status", h.AccountStatus)
	return mux
}

func writeAccountResult(w http.ResponseWriter, report AccountReport, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, report)
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errAccountRequest):
		status = http.StatusBadRequest
	case errors.Is(err, errAccountNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errAccountIncidentOn):
		status = http.StatusConflict
	}
	slog.Warn("account action failed", "error", err)
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
