package weixin

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	loginSessionTTL        = 10 * time.Minute
	loginStatusPollTimeout = 20 * time.Second
)

var (
	ErrLoginSessionNotFound = errors.New("weixin: login session not found")
	ErrLoginSessionExpired  = errors.New("weixin: login session expired")
)

type LoginSession struct {
	ID            string
	QRCodeURL     string
	Status        string
	ExpiresAt     time.Time
	Installation  *Installation
	JustConfirmed bool
}

type loginState struct {
	LoginSession
	QRCode          string
	BaseURL         string
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
}

type LoginService struct {
	mu       sync.Mutex
	client   *Client
	installs *InstallationService
	sessions map[string]*loginState
	now      func() time.Time
}

func NewLoginService(client *Client, installs *InstallationService) *LoginService {
	return &LoginService{client: client, installs: installs, sessions: make(map[string]*loginState), now: time.Now}
}

func (s *LoginService) Begin(ctx context.Context, workspaceID, agentID, installerUserID pgtype.UUID) (LoginSession, error) {
	qr, err := s.client.GetQRCode(ctx)
	if err != nil {
		return LoginSession{}, err
	}
	if qr.Code == "" {
		return LoginSession{}, errors.New("weixin: QR response missing code")
	}
	id, err := randomHex(24)
	if err != nil {
		return LoginSession{}, err
	}
	content := qr.Content
	if content == "" {
		content = qr.Code
	}
	state := &loginState{
		LoginSession: LoginSession{ID: id, QRCodeURL: content, Status: "waiting", ExpiresAt: s.now().Add(loginSessionTTL)},
		QRCode:       qr.Code, BaseURL: DefaultBaseURL, WorkspaceID: workspaceID, AgentID: agentID, InstallerUserID: installerUserID,
	}
	s.mu.Lock()
	s.sessions[id] = state
	s.mu.Unlock()
	return state.LoginSession, nil
}

func (s *LoginService) Status(ctx context.Context, id string, workspaceID pgtype.UUID) (LoginSession, error) {
	s.mu.Lock()
	state := s.sessions[id]
	if state == nil || state.WorkspaceID != workspaceID {
		s.mu.Unlock()
		return LoginSession{}, ErrLoginSessionNotFound
	}
	if s.now().After(state.ExpiresAt) {
		delete(s.sessions, id)
		s.mu.Unlock()
		return LoginSession{}, ErrLoginSessionExpired
	}
	if state.Status == "confirmed" {
		out := state.LoginSession
		out.JustConfirmed = false
		s.mu.Unlock()
		return out, nil
	}
	baseURL, code := state.BaseURL, state.QRCode
	s.mu.Unlock()

	pollCtx, cancel := context.WithTimeout(ctx, loginStatusPollTimeout)
	status, err := s.client.GetQRCodeStatus(pollCtx, baseURL, code)
	cancel()
	if err != nil {
		// iLink holds this request open while the QR remains unscanned. Return
		// the current state before the outer HTTP request reaches its deadline;
		// the browser will issue the next poll. Preserve caller cancellation so
		// navigation and shutdown still stop work immediately.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			state = s.sessions[id]
			if state == nil {
				return LoginSession{}, ErrLoginSessionNotFound
			}
			out := state.LoginSession
			out.JustConfirmed = false
			return out, nil
		}
		return LoginSession{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state = s.sessions[id]
	if state == nil {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	switch status.Status {
	case "scaned", "scanned":
		state.Status = "scanned"
	case "scaned_but_redirect":
		if status.RedirectHost != "" {
			redirectURL, err := normalizeBaseURL("https://" + status.RedirectHost)
			if err != nil {
				return LoginSession{}, err
			}
			state.BaseURL = redirectURL
		}
		state.Status = "waiting"
	case "expired":
		state.Status = "expired"
	case "confirmed":
		if state.Status == "confirmed" {
			out := state.LoginSession
			out.JustConfirmed = false
			return out, nil
		}
		if status.BotID == "" || status.BotToken == "" || status.WeixinUserID == "" {
			return LoginSession{}, errors.New("weixin: confirmed QR response is incomplete")
		}
		inst, err := s.installs.Upsert(ctx, InstallationParams{
			WorkspaceID: state.WorkspaceID, AgentID: state.AgentID, InstallerUserID: state.InstallerUserID,
			BotID: status.BotID, WeixinUserID: status.WeixinUserID, BaseURL: status.BaseURL, Token: status.BotToken,
		})
		if err != nil {
			return LoginSession{}, err
		}
		state.Status = "confirmed"
		state.Installation = &inst
		state.JustConfirmed = true
	default:
		state.Status = "waiting"
	}
	return state.LoginSession, nil
}
