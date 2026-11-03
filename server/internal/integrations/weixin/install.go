package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	ErrNotFound    = errors.New("weixin: connection or login session not found")
	ErrBusy        = errors.New("weixin: login is already being checked")
	ErrConflict    = errors.New("weixin: this bot is already connected; disconnect it before changing its agent")
	ErrInvalidCode = errors.New("weixin: enter the numeric code shown on your phone")
	ErrAccess      = errors.New("weixin: workspace or agent access changed")
)

type loginState struct {
	QRCode  string `json:"qrcode"`
	Content string `json:"content"`
	BaseURL string `json:"base_url"`
}

// LoginView never contains bot credentials, the polling QR value, or a code.
// Only the initiating Multica user can retrieve this short-lived view.
type LoginView struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	QRCodeURL      string `json:"qr_code_url,omitempty"`
	InstallationID string `json:"installation_id,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

type InstallService struct {
	q       *db.Queries
	tx      engine.TxStarter
	box     *secretbox.Box
	client  *http.Client
	apiBase string // test-only transport override
}

func NewInstallService(q *db.Queries, tx engine.TxStarter, box *secretbox.Box) (*InstallService, error) {
	if q == nil || tx == nil || box == nil {
		return nil, errors.New("weixin: queries, transactions and encryption are required")
	}
	return &InstallService{q: q, tx: tx, box: box}, nil
}

func (s *InstallService) api(base, token string) *apiClient {
	if s.apiBase != "" {
		base = s.apiBase
	}
	return newAPI(base, token, s.client)
}

func (s *InstallService) Begin(ctx context.Context, workspace, agent, initiator pgtype.UUID) (LoginView, error) {
	qr, err := s.api(DefaultBaseURL, "").QRCode(ctx)
	if err != nil {
		return LoginView{}, err
	}
	raw, _ := json.Marshal(loginState{QRCode: qr.QRCode, Content: qr.Content, BaseURL: DefaultBaseURL})
	sealed, err := s.box.Seal(raw)
	if err != nil {
		return LoginView{}, err
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return LoginView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	// Serialize a user's repeated Connect clicks; the newest QR cancels the old
	// one even when the two requests land on different replicas.
	if err := q.LockChannelInstallationAppIDSlot(ctx, db.LockChannelInstallationAppIDSlotParams{ChannelType: "weixin-login", AppID: util.UUIDToString(agent) + ":" + util.UUIDToString(initiator)}); err != nil {
		return LoginView{}, err
	}
	if err := q.CancelPreviousWeixinLogins(ctx, db.CancelPreviousWeixinLoginsParams{WorkspaceID: workspace, AgentID: agent, InitiatorUserID: initiator}); err != nil {
		return LoginView{}, err
	}
	row, err := q.CreateWeixinLoginSession(ctx, db.CreateWeixinLoginSessionParams{ID: dbid.NewV7(), WorkspaceID: workspace, AgentID: agent, InitiatorUserID: initiator, StateEncrypted: sealed})
	if err != nil {
		return LoginView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LoginView{}, err
	}
	return s.view(row)
}

func (s *InstallService) view(row db.WeixinLoginSession) (LoginView, error) {
	v := LoginView{ID: util.UUIDToString(row.ID), Status: row.Status, ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339)}
	if row.InstallationID.Valid {
		v.InstallationID = util.UUIDToString(row.InstallationID)
	}
	if pendingLogin(row.Status) && !row.ExpiresAt.Time.After(time.Now()) {
		v.Status = "expired"
	}
	if pendingLogin(v.Status) {
		state, err := s.openState(row.StateEncrypted)
		if err != nil {
			return LoginView{}, err
		}
		v.QRCodeURL = state.Content
	}
	return v, nil
}

func pendingLogin(status string) bool {
	return status == "wait" || status == "scaned" || status == "need_verifycode" || status == "scaned_but_redirect"
}

func (s *InstallService) openState(sealed []byte) (loginState, error) {
	var state loginState
	raw, err := s.box.Open(sealed)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, ErrProtocol
	}
	if state.QRCode == "" {
		return state, ErrProtocol
	}
	if _, err := validateBaseURL(state.BaseURL); err != nil {
		return state, err
	}
	return state, nil
}

func (s *InstallService) Get(ctx context.Context, session, workspace, initiator pgtype.UUID) (LoginView, error) {
	row, err := s.q.GetWeixinLoginSession(ctx, db.GetWeixinLoginSessionParams{ID: session, WorkspaceID: workspace, InitiatorUserID: initiator})
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginView{}, ErrNotFound
	}
	if err != nil {
		return LoginView{}, err
	}
	return s.view(row)
}

func (s *InstallService) Poll(ctx context.Context, session, workspace, initiator pgtype.UUID, code string) (LoginView, error) {
	code = strings.TrimSpace(code)
	if len(code) > 12 {
		return LoginView{}, ErrInvalidCode
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return LoginView{}, ErrInvalidCode
		}
	}
	lease := dbid.NewV7()
	row, err := s.q.ClaimWeixinLoginSession(ctx, db.ClaimWeixinLoginSessionParams{ID: session, WorkspaceID: workspace, InitiatorUserID: initiator, LeaseToken: lease})
	if errors.Is(err, pgx.ErrNoRows) {
		view, err := s.Get(ctx, session, workspace, initiator)
		if err == nil && pendingLogin(view.Status) {
			err = ErrBusy
		}
		return view, err
	}
	if err != nil {
		return LoginView{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.q.ReleaseWeixinLoginSession(cleanup, db.ReleaseWeixinLoginSessionParams{ID: session, LeaseToken: lease})
	}()
	state, err := s.openState(row.StateEncrypted)
	if err != nil {
		return LoginView{}, err
	}
	status, err := s.api(state.BaseURL, "").QRStatus(ctx, state.QRCode, code)
	if err != nil {
		return LoginView{}, err
	}
	if status.Status == "scaned_but_redirect" {
		state.BaseURL, err = validateBaseURL("https://" + status.RedirectHost)
		if err != nil {
			return LoginView{}, err
		}
	}
	if status.Status == "confirmed" {
		if err := s.complete(ctx, row, status); err != nil {
			return LoginView{}, err
		}
	} else {
		var sealed []byte
		if pendingLogin(status.Status) {
			raw, _ := json.Marshal(state)
			sealed, err = s.box.Seal(raw)
			if err != nil {
				return LoginView{}, err
			}
		} else {
			sealed = []byte{}
		}
		if _, err := s.q.UpdateWeixinLoginSession(ctx, db.UpdateWeixinLoginSessionParams{ID: session, LeaseToken: lease, Status: status.Status, StateEncrypted: sealed}); err != nil {
			return LoginView{}, err
		}
	}
	return s.Get(ctx, session, workspace, initiator)
}

func (s *InstallService) complete(ctx context.Context, session db.WeixinLoginSession, result qrStatus) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err := q.LockWeixinLoginSession(ctx, db.LockWeixinLoginSessionParams{ID: session.ID, LeaseToken: session.LeaseToken}); err != nil {
		return ErrNotFound
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: session.InitiatorUserID, WorkspaceID: session.WorkspaceID})
	if err != nil || member.Role != "owner" && member.Role != "admin" {
		return ErrAccess
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: session.AgentID, WorkspaceID: session.WorkspaceID})
	if err != nil || agent.ArchivedAt.Valid {
		return ErrAccess
	}
	if err := q.LockChannelInstallationAppIDSlot(ctx, db.LockChannelInstallationAppIDSlotParams{ChannelType: string(TypeWeixin), AppID: result.BotID}); err != nil {
		return err
	}
	owner, err := q.GetChannelInstallationSlotOwnerByAppID(ctx, db.GetChannelInstallationSlotOwnerByAppIDParams{ChannelType: string(TypeWeixin), AppID: result.BotID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && owner.Status == "active" && owner.WorkspaceExists && owner.AgentExists && (owner.WorkspaceID != session.WorkspaceID || owner.AgentID != session.AgentID) {
		return ErrConflict
	}
	if _, err := q.ReclaimDeadChannelInstallationByAppID(ctx, db.ReclaimDeadChannelInstallationByAppIDParams{ChannelType: string(TypeWeixin), AppID: result.BotID, WorkspaceID: session.WorkspaceID, AgentID: session.AgentID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	old, err := q.GetWeixinInstallationForAgent(ctx, db.GetWeixinInstallationForAgentParams{WorkspaceID: session.WorkspaceID, AgentID: session.AgentID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		previous := DecodePublicConfig(old.Config)
		if old.Status == "active" && (previous.BotID != result.BotID || previous.UserID != result.UserID || old.InstallerUserID != session.InitiatorUserID) {
			return ErrConflict
		}
		if previous.BotID != result.BotID || previous.UserID != result.UserID || old.InstallerUserID != session.InitiatorUserID {
			if _, err := q.ClearChannelInstallationBotScopedRows(ctx, old.ID); err != nil {
				return err
			}
		}
	}
	sealed, err := s.box.Seal([]byte(result.Token))
	if err != nil {
		return err
	}
	config, _ := json.Marshal(installConfig{AppID: result.BotID, UserID: result.UserID, BaseURL: result.BaseURL, TokenEncrypted: base64.StdEncoding.EncodeToString(sealed)})
	inst, err := q.UpsertChannelInstallation(ctx, db.UpsertChannelInstallationParams{WorkspaceID: session.WorkspaceID, AgentID: session.AgentID, ChannelType: string(TypeWeixin), Config: config, InstallerUserID: session.InitiatorUserID})
	if err != nil {
		return err
	}
	if _, err := q.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{WorkspaceID: session.WorkspaceID, MulticaUserID: session.InitiatorUserID, InstallationID: inst.ID, ChannelType: string(TypeWeixin), ChannelUserID: result.UserID, Config: []byte(`{}`)}); err != nil {
		return err
	}
	if err := q.ResetWeixinAccountState(ctx, db.ResetWeixinAccountStateParams{InstallationID: inst.ID, BotID: result.BotID, UserID: result.UserID}); err != nil {
		return err
	}
	if _, err := q.UpdateWeixinLoginSession(ctx, db.UpdateWeixinLoginSessionParams{ID: session.ID, LeaseToken: session.LeaseToken, Status: "confirmed", StateEncrypted: []byte{}, InstallationID: inst.ID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *InstallService) Cancel(ctx context.Context, session, workspace, initiator pgtype.UUID) error {
	_, err := s.q.CancelWeixinLoginSession(ctx, db.CancelWeixinLoginSessionParams{ID: session, WorkspaceID: workspace, InitiatorUserID: initiator})
	return err
}

func (s *InstallService) List(ctx context.Context, workspace pgtype.UUID) ([]db.ChannelInstallation, error) {
	return s.q.ListChannelInstallationsByWorkspace(ctx, db.ListChannelInstallationsByWorkspaceParams{WorkspaceID: workspace, ChannelType: string(TypeWeixin)})
}

func (s *InstallService) Revoke(ctx context.Context, installation, workspace pgtype.UUID) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{ID: installation, WorkspaceID: workspace, ChannelType: string(TypeWeixin)})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := q.SetChannelInstallationStatus(ctx, db.SetChannelInstallationStatusParams{ID: row.ID, Status: "revoked"}); err != nil {
		return err
	}
	if err := q.DeleteWeixinAccountState(ctx, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *InstallService) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		_ = s.q.PurgeWeixinState(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
