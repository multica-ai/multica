package qianwen

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	taskservice "github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testConnectionID     = "qwc_AAAAAAAAAAAAAAAAAAAAAAAA"
	testAccessToken      = "qws_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testWrongAccessToken = "qws_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
	testRequestID        = "550e8400-e29b-41d4-a716-446655440000"
)

type fakeServiceQueries struct {
	trace *[]string

	upsertResult db.ChannelInstallation
	upsertErr    error
	upsertArg    db.InstallQianwenPersonalParams
	upsertCalls  int

	installation      db.ChannelInstallation
	installationErr   error
	installationArg   db.GetChannelInstallationByAppIDParams
	installationCalls int
	member            db.Member
	memberErr         error
	memberArg         db.GetMemberByUserAndWorkspaceParams
	memberCalls       int
	invocationUser    pgtype.UUID
	invocationErr     error
	invocationArg     db.GetActiveQianwenInvocationUserParams
	invocationCalls   int

	claimResult   db.QianwenSkillRequest
	claimErr      error
	claimArg      db.ClaimQianwenRequestParams
	claimCalls    int
	existing      db.QianwenSkillRequest
	existingErr   error
	existingArg   db.GetQianwenRequestParams
	existingCalls int

	setSessionRows    int64
	setSessionErr     error
	setSessionArg     db.SetQianwenRequestSessionParams
	setSessionCalls   int
	releaseRows       int64
	releaseErr        error
	releaseArg        db.ReleaseQianwenRequestClaimParams
	releaseCalls      int
	releaseContextErr error

	chatSession      db.ChatSession
	chatSessionErr   error
	chatSessionArg   pgtype.UUID
	chatSessionCalls int
	agent            db.Agent
	agentErr         error
	agentArg         pgtype.UUID
	agentCalls       int

	requestStatus      db.GetQianwenRequestStatusRow
	requestStatusErr   error
	requestStatusArg   db.GetQianwenRequestStatusParams
	requestStatusCalls int

	pairingResult db.QianwenPairingCode
	pairingErrs   []error
	pairingArgs   []db.UpsertQianwenPairingCodeParams
}

func (f *fakeServiceQueries) UpsertQianwenPairingCode(_ context.Context, arg db.UpsertQianwenPairingCodeParams) (db.QianwenPairingCode, error) {
	f.pairingArgs = append(f.pairingArgs, arg)
	if len(f.pairingErrs) > 0 {
		err := f.pairingErrs[0]
		f.pairingErrs = f.pairingErrs[1:]
		if err != nil {
			return db.QianwenPairingCode{}, err
		}
	}
	return f.pairingResult, nil
}

func (f *fakeServiceQueries) record(event string) {
	if f.trace != nil {
		*f.trace = append(*f.trace, event)
	}
}

func (f *fakeServiceQueries) InstallQianwenPersonal(_ context.Context, arg db.InstallQianwenPersonalParams) (db.ChannelInstallation, error) {
	f.upsertCalls++
	f.upsertArg = arg
	if f.upsertErr != nil {
		return db.ChannelInstallation{}, f.upsertErr
	}
	row := f.upsertResult
	if row.Config == nil {
		row.Config = append([]byte(nil), arg.Config...)
	}
	return row, nil
}

func (f *fakeServiceQueries) ListChannelInstallationsByWorkspace(context.Context, db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return nil, nil
}

func (f *fakeServiceQueries) GetChannelInstallationInWorkspace(context.Context, db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, nil
}

func (f *fakeServiceQueries) GetChannelInstallationByAppID(_ context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.record("get_installation")
	f.installationCalls++
	f.installationArg = arg
	return f.installation, f.installationErr
}

func (f *fakeServiceQueries) RevokeQianwenInstallation(context.Context, pgtype.UUID) (int64, error) {
	return 1, nil
}

func (f *fakeServiceQueries) GetMemberByUserAndWorkspace(_ context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	f.record("get_member")
	f.memberCalls++
	f.memberArg = arg
	return f.member, f.memberErr
}

func (f *fakeServiceQueries) GetActiveQianwenInvocationUser(_ context.Context, arg db.GetActiveQianwenInvocationUserParams) (pgtype.UUID, error) {
	f.record("get_invocation_user")
	f.invocationCalls++
	f.invocationArg = arg
	return f.invocationUser, f.invocationErr
}

func (f *fakeServiceQueries) ClaimQianwenRequest(_ context.Context, arg db.ClaimQianwenRequestParams) (db.QianwenSkillRequest, error) {
	f.record("claim")
	f.claimCalls++
	f.claimArg = arg
	return f.claimResult, f.claimErr
}

func (f *fakeServiceQueries) GetQianwenRequest(_ context.Context, arg db.GetQianwenRequestParams) (db.QianwenSkillRequest, error) {
	f.record("get_request")
	f.existingCalls++
	f.existingArg = arg
	return f.existing, f.existingErr
}

func (f *fakeServiceQueries) SetQianwenRequestSession(_ context.Context, arg db.SetQianwenRequestSessionParams) (int64, error) {
	f.record("set_session")
	f.setSessionCalls++
	f.setSessionArg = arg
	return f.setSessionRows, f.setSessionErr
}

func (f *fakeServiceQueries) ReleaseQianwenRequestClaim(ctx context.Context, arg db.ReleaseQianwenRequestClaimParams) (int64, error) {
	f.record("release")
	f.releaseCalls++
	f.releaseArg = arg
	f.releaseContextErr = ctx.Err()
	return f.releaseRows, f.releaseErr
}

func (f *fakeServiceQueries) GetChatSession(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	f.record("get_session")
	f.chatSessionCalls++
	f.chatSessionArg = id
	return f.chatSession, f.chatSessionErr
}

func (f *fakeServiceQueries) GetAgent(_ context.Context, id pgtype.UUID) (db.Agent, error) {
	f.record("get_agent")
	f.agentCalls++
	f.agentArg = id
	return f.agent, f.agentErr
}

func (f *fakeServiceQueries) GetQianwenRequestStatus(_ context.Context, arg db.GetQianwenRequestStatusParams) (db.GetQianwenRequestStatusRow, error) {
	f.record("status")
	f.requestStatusCalls++
	f.requestStatusArg = arg
	return f.requestStatus, f.requestStatusErr
}

type fakeSessionEnsurer struct {
	trace *[]string
	id    pgtype.UUID
	err   error
	arg   engine.EnsureSessionInput
	calls int
}

func (f *fakeSessionEnsurer) EnsureSession(_ context.Context, arg engine.EnsureSessionInput) (pgtype.UUID, error) {
	if f.trace != nil {
		*f.trace = append(*f.trace, "ensure_session")
	}
	f.calls++
	f.arg = arg
	return f.id, f.err
}

type fakeTaskSender struct {
	trace *[]string

	result *taskservice.DirectChatSendResult
	err    error
	calls  int

	session   db.ChatSession
	agent     db.Agent
	initiator pgtype.UUID
	content   string
	finalize  taskservice.ChannelDirectChatFinalize
}

func (f *fakeTaskSender) SendChannelDirectChatMessage(_ context.Context, session db.ChatSession, agent db.Agent, initiator pgtype.UUID, content string, finalize taskservice.ChannelDirectChatFinalize) (*taskservice.DirectChatSendResult, error) {
	if f.trace != nil {
		*f.trace = append(*f.trace, "send")
	}
	f.calls++
	f.session = session
	f.agent = agent
	f.initiator = initiator
	f.content = content
	f.finalize = finalize
	return f.result, f.err
}

func TestInstallPersonalPersistsOnlyAccessTokenHash(t *testing.T) {
	t.Parallel()

	queries := &fakeServiceQueries{upsertResult: db.ChannelInstallation{Status: "active"}}
	service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

	result, err := service.InstallPersonal(context.Background(), testPGUUID(1), testPGUUID(2), testPGUUID(3))
	if err != nil {
		t.Fatalf("InstallPersonal() error = %v", err)
	}
	if !ValidCredentialShape(result.ConnectionID, result.AccessToken) {
		t.Fatalf("generated credentials have invalid shape: connection=%q token=%q", result.ConnectionID, result.AccessToken)
	}
	if bytes.Contains(queries.upsertArg.Config, []byte(result.AccessToken)) {
		t.Fatal("persisted installation config contains the plaintext access token")
	}

	var stored installConfig
	if err := json.Unmarshal(queries.upsertArg.Config, &stored); err != nil {
		t.Fatalf("decode persisted config: %v", err)
	}
	if stored.AppID != result.ConnectionID || stored.Mode != personalPollingMode {
		t.Fatalf("stored public config = %+v", stored)
	}
	if stored.AccessTokenHash == "" || stored.AccessTokenHash == result.AccessToken {
		t.Fatalf("stored token digest = %q, want a non-plaintext digest", stored.AccessTokenHash)
	}
	if !verifyAccessToken(queries.upsertArg.Config, result.AccessToken) {
		t.Fatal("persisted digest did not verify the one-time access token")
	}
	if verifyAccessToken(queries.upsertArg.Config, testWrongAccessToken) {
		t.Fatal("persisted digest verified a different well-formed token")
	}

	public := DecodePublicConfig(queries.upsertArg.Config)
	encodedPublic, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public config: %v", err)
	}
	if public.ConnectionID != result.ConnectionID || public.Mode != personalPollingMode ||
		bytes.Contains(encodedPublic, []byte(result.AccessToken)) || bytes.Contains(encodedPublic, []byte(stored.AccessTokenHash)) {
		t.Fatalf("public config leaked token material: %s", encodedPublic)
	}
}

func TestVerifyAccessTokenRejectsInvalidCredentialMaterial(t *testing.T) {
	t.Parallel()

	validConfig, err := encodeInstallConfig(testConnectionID, testAccessToken)
	if err != nil {
		t.Fatalf("encodeInstallConfig() error = %v", err)
	}
	wrongModeConfig, err := json.Marshal(installConfig{
		AppID:           testConnectionID,
		AccessTokenHash: hashAccessToken(testAccessToken),
		Mode:            "public",
	})
	if err != nil {
		t.Fatalf("marshal wrong-mode config: %v", err)
	}
	tests := []struct {
		name   string
		config json.RawMessage
		token  string
	}{
		{name: "wrong well formed token", config: validConfig, token: testWrongAccessToken},
		{name: "missing prefix", config: validConfig, token: strings.TrimPrefix(testAccessToken, accessTokenPrefix)},
		{name: "invalid encoding", config: validConfig, token: accessTokenPrefix + "%%%"},
		{name: "malformed config", config: json.RawMessage(`{"access_token_hash":`), token: testAccessToken},
		{name: "missing digest", config: json.RawMessage(`{"app_id":"qwc_test","mode":"personal_polling"}`), token: testAccessToken},
		{name: "wrong mode", config: wrongModeConfig, token: testAccessToken},
		{name: "malformed digest", config: json.RawMessage(`{"access_token_hash":"%%%","mode":"personal_polling"}`), token: testAccessToken},
		{name: "short digest", config: json.RawMessage(`{"access_token_hash":"YQ","mode":"personal_polling"}`), token: testAccessToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if verifyAccessToken(tc.config, tc.token) {
				t.Fatal("verifyAccessToken() = true, want false")
			}
		})
	}
}

func TestSubmitClaimsSessionAndUsesAtomicChannelSender(t *testing.T) {
	t.Parallel()

	trace := []string{}
	queries := authenticatedQueries(t)
	queries.trace = &trace
	sessions := &fakeSessionEnsurer{trace: &trace, id: queries.chatSession.ID}
	tasks := &fakeTaskSender{
		trace:  &trace,
		result: &taskservice.DirectChatSendResult{Task: db.AgentTaskQueue{ID: testPGUUID(9)}},
	}
	service := mustTestService(t, queries, sessions, tasks)

	invocation := signedUnitSubmitInvocation(service, testAccessToken, SubmitRequest{
		RequestID: "  550E8400-E29B-41D4-A716-446655440000  ",
		Query:     "  inspect the build  \n",
	})
	result, err := service.Submit(context.Background(), testConnectionID, testAccessToken, invocation)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result != (SubmitResult{RequestID: testRequestID, Status: "accepted"}) {
		t.Fatalf("Submit() = %+v", result)
	}
	if queries.installationArg != (db.GetChannelInstallationByAppIDParams{ChannelType: string(TypeQianwen), AppID: testConnectionID}) {
		t.Fatalf("installation lookup = %+v", queries.installationArg)
	}
	if queries.memberArg != (db.GetMemberByUserAndWorkspaceParams{
		UserID:      queries.installation.InstallerUserID,
		WorkspaceID: queries.installation.WorkspaceID,
	}) {
		t.Fatalf("membership lookup = %+v", queries.memberArg)
	}
	wantTrace := []string{"get_installation", "get_member", "get_invocation_user", "claim", "ensure_session", "set_session", "get_session", "get_agent", "send"}
	if strings.Join(trace, ",") != strings.Join(wantTrace, ",") {
		t.Fatalf("call order = %v, want %v", trace, wantTrace)
	}

	requestUUID := util.MustParseUUID(testRequestID)
	if queries.claimArg.InstallationID != queries.installation.ID ||
		queries.claimArg.WorkspaceID != queries.installation.WorkspaceID ||
		queries.claimArg.AgentID != queries.installation.AgentID ||
		queries.claimArg.MulticaUserID != queries.invocationUser ||
		queries.claimArg.ConnectionID != testConnectionID ||
		queries.claimArg.AccessTokenHash != hashAccessToken(testAccessToken) ||
		queries.claimArg.OpenUserID != invocation.Identity.OpenUserID ||
		queries.claimArg.OpenUuid != invocation.Identity.OpenUUID ||
		queries.claimArg.RequestID != requestUUID {
		t.Fatalf("claim args = %+v", queries.claimArg)
	}
	wantHash := sha256.Sum256([]byte("inspect the build"))
	if !bytes.Equal(queries.claimArg.QuerySha256, wantHash[:]) {
		t.Fatalf("claim query hash = %x, want %x", queries.claimArg.QuerySha256, wantHash)
	}
	if sessions.arg.WorkspaceID != queries.installation.WorkspaceID ||
		sessions.arg.AgentID != queries.installation.AgentID ||
		sessions.arg.InstallationID != queries.installation.ID ||
		sessions.arg.Sender != queries.invocationUser ||
		sessions.arg.BindingKey != testRequestID || sessions.arg.ChatType != channel.ChatTypeP2P ||
		sessions.arg.Title != "inspect the build" {
		t.Fatalf("EnsureSession input = %+v", sessions.arg)
	}
	if queries.setSessionArg != (db.SetQianwenRequestSessionParams{
		ChatSessionID:  queries.chatSession.ID,
		InstallationID: queries.installation.ID,
		RequestID:      requestUUID,
		MulticaUserID:  queries.invocationUser,
		ClaimToken:     queries.claimResult.ClaimToken,
	}) {
		t.Fatalf("SetQianwenRequestSession args = %+v", queries.setSessionArg)
	}
	if queries.chatSessionArg != queries.chatSession.ID || queries.agentArg != queries.installation.AgentID {
		t.Fatalf("session/agent loads = session %s agent %s", util.UUIDToString(queries.chatSessionArg), util.UUIDToString(queries.agentArg))
	}
	if tasks.session.ID != queries.chatSession.ID || tasks.agent.ID != queries.agent.ID ||
		tasks.initiator != queries.invocationUser || tasks.content != "inspect the build" {
		t.Fatalf("atomic sender input = session %s agent %s initiator %s content %q",
			util.UUIDToString(tasks.session.ID), util.UUIDToString(tasks.agent.ID), util.UUIDToString(tasks.initiator), tasks.content)
	}
	if tasks.finalize == nil {
		t.Fatal("atomic sender received a nil request-ledger finalizer")
	}
	if queries.releaseCalls != 0 {
		t.Fatalf("successful submit released the published claim %d time(s)", queries.releaseCalls)
	}
}

func TestSubmitIdempotentReplayDoesNotStartAnotherTask(t *testing.T) {
	t.Parallel()

	normalizedQuery := "retry the same request"
	tests := []struct {
		name     string
		existing db.QianwenSkillRequest
	}{
		{
			name: "active claim",
			existing: db.QianwenSkillRequest{
				ClaimToken:     testPGUUID(7),
				ClaimExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
			},
		},
		{name: "task already durable", existing: db.QianwenSkillRequest{TaskID: testPGUUID(9)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			queries.claimErr = pgx.ErrNoRows
			queries.existing = tc.existing
			queries.existing.InstallationID = queries.installation.ID
			queries.existing.RequestID = util.MustParseUUID(testRequestID)
			queries.existing.MulticaUserID = queries.invocationUser
			queries.existing.QuerySha256 = queryDigest(normalizedQuery)
			sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
			tasks := &fakeTaskSender{}
			service := mustTestService(t, queries, sessions, tasks)

			invocation := signedUnitSubmitInvocation(service, testAccessToken, SubmitRequest{
				RequestID: strings.ToUpper(testRequestID),
				Query:     "  " + normalizedQuery + "  ",
			})
			got, err := service.Submit(context.Background(), testConnectionID, testAccessToken, invocation)
			if err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
			if got != (SubmitResult{RequestID: testRequestID, Status: "accepted"}) {
				t.Fatalf("Submit() = %+v", got)
			}
			if queries.existingCalls != 1 || sessions.calls != 0 || queries.setSessionCalls != 0 || tasks.calls != 0 || queries.releaseCalls != 0 {
				t.Fatalf("replay work = get %d ensure %d set %d send %d release %d",
					queries.existingCalls, sessions.calls, queries.setSessionCalls, tasks.calls, queries.releaseCalls)
			}
		})
	}
}

func TestSubmitRejectsRequestIDReuseWithDifferentQuery(t *testing.T) {
	t.Parallel()

	queries := authenticatedQueries(t)
	queries.claimErr = pgx.ErrNoRows
	queries.existing = db.QianwenSkillRequest{
		InstallationID: queries.installation.ID,
		RequestID:      util.MustParseUUID(testRequestID),
		MulticaUserID:  queries.invocationUser,
		QuerySha256:    queryDigest("original query"),
		TaskID:         testPGUUID(9),
	}
	sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
	tasks := &fakeTaskSender{}
	service := mustTestService(t, queries, sessions, tasks)

	invocation := signedUnitSubmitInvocation(service, testAccessToken, SubmitRequest{
		RequestID: testRequestID,
		Query:     "different query",
	})
	_, err := service.Submit(context.Background(), testConnectionID, testAccessToken, invocation)
	if !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("Submit() error = %v, want ErrRequestConflict", err)
	}
	if sessions.calls != 0 || tasks.calls != 0 || queries.releaseCalls != 0 {
		t.Fatalf("conflict performed ensure=%d send=%d release=%d", sessions.calls, tasks.calls, queries.releaseCalls)
	}
}

func TestSubmitMapsMissingClaimAndLedgerAfterOwnerTeardownToUnauthorized(t *testing.T) {
	t.Parallel()

	queries := authenticatedQueries(t)
	queries.claimErr = pgx.ErrNoRows
	queries.existingErr = pgx.ErrNoRows
	sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
	tasks := &fakeTaskSender{}
	service := mustTestService(t, queries, sessions, tasks)

	invocation := signedUnitSubmitInvocation(service, testAccessToken, SubmitRequest{
		RequestID: testRequestID,
		Query:     "request raced with teardown",
	})
	_, err := service.Submit(context.Background(), testConnectionID, testAccessToken, invocation)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Submit() error = %v, want ErrUnauthorized", err)
	}
	if sessions.calls != 0 || tasks.calls != 0 || queries.releaseCalls != 0 {
		t.Fatalf("teardown race performed ensure=%d send=%d release=%d", sessions.calls, tasks.calls, queries.releaseCalls)
	}
}

func TestSubmitRejectsInvalidRequestsBeforeClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request SubmitRequest
		wantErr error
	}{
		{name: "invalid request id", request: SubmitRequest{RequestID: "not-a-uuid", Query: "hello"}, wantErr: ErrInvalidRequest},
		{name: "zero request id", request: SubmitRequest{RequestID: "00000000-0000-0000-0000-000000000000", Query: "hello"}, wantErr: ErrInvalidRequest},
		{name: "empty query", request: SubmitRequest{RequestID: testRequestID, Query: " \n\t "}, wantErr: ErrInvalidRequest},
		{name: "query too large", request: SubmitRequest{RequestID: testRequestID, Query: strings.Repeat("a", maxQueryBytes+1)}, wantErr: ErrInvalidRequest},
		{name: "invalid utf8", request: SubmitRequest{RequestID: testRequestID, Query: string([]byte{0xff})}, wantErr: ErrInvalidRequest},
		{name: "issue command", request: SubmitRequest{RequestID: testRequestID, Query: "/issue glasses task"}, wantErr: ErrUnsupportedCommand},
		{name: "fresh session command", request: SubmitRequest{RequestID: testRequestID, Query: "/new inspect"}, wantErr: ErrUnsupportedCommand},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
			tasks := &fakeTaskSender{}
			service := mustTestService(t, queries, sessions, tasks)

			_, err := service.Submit(context.Background(), testConnectionID, testAccessToken,
				signedUnitSubmitInvocation(service, testAccessToken, tc.request))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Submit() error = %v, want errors.Is(%v)", err, tc.wantErr)
			}
			if queries.claimCalls != 0 || sessions.calls != 0 || tasks.calls != 0 {
				t.Fatalf("invalid request performed claim=%d ensure=%d send=%d", queries.claimCalls, sessions.calls, tasks.calls)
			}
		})
	}
}

func TestSubmitReleasesClaimOnPrePublishFailure(t *testing.T) {
	t.Parallel()

	ensureErr := errors.New("ensure failed")
	setErr := errors.New("set session failed")
	senderErr := errors.New("runtime unavailable: sensitive detail")
	authorityErr := errors.Join(errors.New("finalizer rejected authority"), ErrUnauthorized)
	tests := []struct {
		name            string
		configure       func(*fakeServiceQueries, *fakeSessionEnsurer, *fakeTaskSender)
		wantErr         error
		wantSenderCalls int
	}{
		{
			name: "ensure session error",
			configure: func(_ *fakeServiceQueries, sessions *fakeSessionEnsurer, _ *fakeTaskSender) {
				sessions.err = ensureErr
			},
			wantErr: ensureErr,
		},
		{
			name: "set session updates zero rows",
			configure: func(queries *fakeServiceQueries, _ *fakeSessionEnsurer, _ *fakeTaskSender) {
				queries.setSessionRows = 0
			},
			wantErr: ErrTaskNotQueued,
		},
		{
			name: "set session error",
			configure: func(queries *fakeServiceQueries, _ *fakeSessionEnsurer, _ *fakeTaskSender) {
				queries.setSessionErr = setErr
			},
			wantErr: setErr,
		},
		{
			name: "task sender deadline",
			configure: func(_ *fakeServiceQueries, _ *fakeSessionEnsurer, tasks *fakeTaskSender) {
				tasks.err = context.DeadlineExceeded
			},
			wantErr:         context.DeadlineExceeded,
			wantSenderCalls: 1,
		},
		{
			name: "task sender error",
			configure: func(_ *fakeServiceQueries, _ *fakeSessionEnsurer, tasks *fakeTaskSender) {
				tasks.err = senderErr
			},
			wantErr:         ErrTaskNotQueued,
			wantSenderCalls: 1,
		},
		{
			name: "task sender authority changed",
			configure: func(_ *fakeServiceQueries, _ *fakeSessionEnsurer, tasks *fakeTaskSender) {
				tasks.err = authorityErr
			},
			wantErr:         ErrUnauthorized,
			wantSenderCalls: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
			tasks := &fakeTaskSender{result: &taskservice.DirectChatSendResult{Task: db.AgentTaskQueue{ID: testPGUUID(9)}}}
			tc.configure(queries, sessions, tasks)
			service := mustTestService(t, queries, sessions, tasks)

			invocation := signedUnitSubmitInvocation(service, testAccessToken, SubmitRequest{
				RequestID: testRequestID,
				Query:     "run the task",
			})
			_, err := service.Submit(context.Background(), testConnectionID, testAccessToken, invocation)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Submit() error = %v, want errors.Is(%v)", err, tc.wantErr)
			}
			if tasks.calls != tc.wantSenderCalls {
				t.Fatalf("sender calls = %d, want %d", tasks.calls, tc.wantSenderCalls)
			}
			if tasks.calls > 0 && tasks.finalize == nil {
				t.Fatal("task sender received nil ledger finalizer")
			}
			if queries.releaseCalls != 1 {
				t.Fatalf("release calls = %d, want 1", queries.releaseCalls)
			}
			wantRelease := db.ReleaseQianwenRequestClaimParams{
				InstallationID: queries.installation.ID,
				RequestID:      util.MustParseUUID(testRequestID),
				MulticaUserID:  queries.invocationUser,
				ClaimToken:     queries.claimResult.ClaimToken,
			}
			if queries.releaseArg != wantRelease {
				t.Fatalf("release args = %+v, want %+v", queries.releaseArg, wantRelease)
			}
			if queries.releaseContextErr != nil {
				t.Fatalf("detached release context error = %v", queries.releaseContextErr)
			}
		})
	}
}

func TestSubmitRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		connectionID string
		token        string
		configure    func(*fakeServiceQueries)
		wantLookups  int
		wantMembers  int
	}{
		{name: "invalid connection id", connectionID: "qwc_invalid", token: testAccessToken},
		{name: "unknown connection", connectionID: testConnectionID, token: testAccessToken, configure: func(q *fakeServiceQueries) { q.installationErr = pgx.ErrNoRows }, wantLookups: 1},
		{name: "wrong well formed token", connectionID: testConnectionID, token: testWrongAccessToken, wantLookups: 1},
		{name: "revoked installation", connectionID: testConnectionID, token: testAccessToken, configure: func(q *fakeServiceQueries) { q.installation.Status = "revoked" }, wantLookups: 1},
		{name: "installer removed from workspace", connectionID: testConnectionID, token: testAccessToken, configure: func(q *fakeServiceQueries) { q.memberErr = pgx.ErrNoRows }, wantLookups: 1, wantMembers: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			if tc.configure != nil {
				tc.configure(queries)
			}
			sessions := &fakeSessionEnsurer{id: queries.chatSession.ID}
			tasks := &fakeTaskSender{}
			service := mustTestService(t, queries, sessions, tasks)

			_, err := service.Submit(context.Background(), tc.connectionID, tc.token,
				signedUnitSubmitInvocation(service, tc.token, SubmitRequest{RequestID: testRequestID, Query: "hello"}))
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("Submit() error = %v, want ErrUnauthorized", err)
			}
			if queries.installationCalls != tc.wantLookups || queries.memberCalls != tc.wantMembers {
				t.Fatalf("auth lookups = installation %d member %d, want %d/%d",
					queries.installationCalls, queries.memberCalls, tc.wantLookups, tc.wantMembers)
			}
			if queries.claimCalls != 0 || sessions.calls != 0 || tasks.calls != 0 {
				t.Fatalf("unauthorized request performed claim=%d ensure=%d send=%d", queries.claimCalls, sessions.calls, tasks.calls)
			}
		})
	}
}

func TestStatusUsesInstallationAndUUIDScopeAndMapsLifecycle(t *testing.T) {
	t.Parallel()

	taskID := testPGUUID(9)
	tests := []struct {
		name      string
		row       db.GetQianwenRequestStatusRow
		wantState string
		wantOut   string
	}{
		{name: "queued", row: db.GetQianwenRequestStatusRow{TaskID: taskID, TaskStatus: "deferred"}, wantState: "queued"},
		{name: "running", row: db.GetQianwenRequestStatusRow{TaskID: taskID, TaskStatus: "waiting_local_directory"}, wantState: "running"},
		{name: "completed", row: db.GetQianwenRequestStatusRow{TaskID: taskID, TaskStatus: "completed", Output: "build passed"}, wantState: "completed", wantOut: "build passed"},
		{name: "cancelled", row: db.GetQianwenRequestStatusRow{TaskID: taskID, TaskStatus: "cancelled"}, wantState: "cancelled"},
		{name: "unknown is not leaked", row: db.GetQianwenRequestStatusRow{TaskID: taskID, TaskStatus: "provider_secret_state"}, wantState: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			queries.requestStatus = tc.row
			service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

			invocation := signedUnitStatusInvocation(service, testAccessToken, strings.ToUpper(testRequestID))
			got, err := service.Status(context.Background(), testConnectionID, testAccessToken, invocation)
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if got.RequestID != testRequestID || got.Status != tc.wantState || got.Output != tc.wantOut {
				t.Fatalf("Status() = %+v", got)
			}
			wantArg := db.GetQianwenRequestStatusParams{
				ConnectionID:    testConnectionID,
				AccessTokenHash: hashAccessToken(testAccessToken),
				InstallationID:  queries.installation.ID,
				RequestID:       util.MustParseUUID(testRequestID),
				MulticaUserID:   queries.invocationUser,
				OpenUserID:      invocation.Identity.OpenUserID,
				OpenUuid:        invocation.Identity.OpenUUID,
			}
			if queries.requestStatusArg != wantArg {
				t.Fatalf("status lookup = %+v, want %+v", queries.requestStatusArg, wantArg)
			}
		})
	}
}

func TestFailedStatusDoesNotExposeTranscriptOrRawExecutionDetails(t *testing.T) {
	t.Parallel()

	const sensitive = "provider error: token=super-secret; stack trace follows"
	queries := authenticatedQueries(t)
	queries.requestStatus = db.GetQianwenRequestStatusRow{
		TaskID:     testPGUUID(9),
		TaskStatus: "failed",
		Output:     sensitive,
		OutputKind: "error",
	}
	service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

	got, err := service.Status(context.Background(), testConnectionID, testAccessToken,
		signedUnitStatusInvocation(service, testAccessToken, testRequestID))
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got.Status != "failed" || got.Output != "" {
		t.Fatalf("Status() = %+v, want failed with no output", got)
	}
	if got.Message != "The task failed. Open Multica to inspect the execution details." {
		t.Fatalf("failure message = %q", got.Message)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if bytes.Contains(encoded, []byte(sensitive)) || bytes.Contains(encoded, []byte("super-secret")) {
		t.Fatalf("public status leaked execution details: %s", encoded)
	}
}

func TestStatusTruncatesCompletedOutputByRune(t *testing.T) {
	t.Parallel()

	queries := authenticatedQueries(t)
	queries.requestStatus = db.GetQianwenRequestStatusRow{
		TaskID:     testPGUUID(9),
		TaskStatus: "completed",
		Output:     strings.Repeat("界", maxStatusOutputRunes+1),
	}
	service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

	got, err := service.Status(context.Background(), testConnectionID, testAccessToken,
		signedUnitStatusInvocation(service, testAccessToken, testRequestID))
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !got.Truncated || !strings.HasSuffix(got.Output, "…") {
		t.Fatalf("Status() truncation = truncated %v", got.Truncated)
	}
	if runeCount := len([]rune(got.Output)); runeCount != maxStatusOutputRunes+1 {
		t.Fatalf("output rune count = %d, want %d including ellipsis", runeCount, maxStatusOutputRunes+1)
	}
}

func TestStatusWithoutTaskFollowsDatabaseClaimLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		claimActive bool
		wantStatus  string
		wantMessage bool
	}{
		{name: "active claim remains accepted", claimActive: true, wantStatus: "accepted"},
		{name: "expired or released claim is failed", wantStatus: "failed", wantMessage: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			queries := authenticatedQueries(t)
			queries.requestStatus = db.GetQianwenRequestStatusRow{ClaimActive: tc.claimActive}
			service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

			got, err := service.Status(context.Background(), testConnectionID, testAccessToken,
				signedUnitStatusInvocation(service, testAccessToken, testRequestID))
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if got.Status != tc.wantStatus || (got.Message != "") != tc.wantMessage || got.TaskID != "" || got.Output != "" {
				t.Fatalf("Status() = %+v", got)
			}
		})
	}
}

func TestStatusReturnsNotFoundWithoutLeakingDatabaseError(t *testing.T) {
	t.Parallel()

	queries := authenticatedQueries(t)
	queries.requestStatusErr = pgx.ErrNoRows
	service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})

	_, err := service.Status(context.Background(), testConnectionID, testAccessToken,
		signedUnitStatusInvocation(service, testAccessToken, testRequestID))
	if !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("Status() error = %v, want ErrRequestNotFound", err)
	}
}

func TestPairingIsUnavailableWithoutStrongDeploymentSecret(t *testing.T) {
	queries := authenticatedQueries(t)
	service, err := newService(queries, &fakeSessionEnsurer{}, &fakeTaskSender{}, nil)
	if err != nil {
		t.Fatalf("newService() error = %v; the polling bridge must remain available when only pairing is disabled", err)
	}
	if service.PairingSupported() {
		t.Fatal("PairingSupported() = true without a deployment secret, want false")
	}

	_, err = service.MintPairingCode(
		context.Background(),
		queries.installation.WorkspaceID,
		queries.installation.ID,
		queries.installation.InstallerUserID,
	)
	if !errors.Is(err, ErrPairingUnavailable) {
		t.Fatalf("MintPairingCode() error = %v, want ErrPairingUnavailable", err)
	}

	_, err = service.InstallPersonal(
		context.Background(),
		queries.installation.WorkspaceID,
		queries.installation.AgentID,
		queries.installation.InstallerUserID,
	)
	if !errors.Is(err, ErrPairingUnavailable) {
		t.Fatalf("InstallPersonal() error = %v, want ErrPairingUnavailable", err)
	}
	if queries.upsertCalls != 0 {
		t.Fatalf("installation writes = %d without pairing capability, want 0", queries.upsertCalls)
	}
}

func TestPairingSupportedRequiresCompleteServiceCapability(t *testing.T) {
	queries := authenticatedQueries(t)
	service, err := newService(queries, &fakeSessionEnsurer{}, &fakeTaskSender{}, []byte("qianwen-unit-test-deployment-secret"))
	if err != nil {
		t.Fatalf("newService() error = %v", err)
	}
	if service.PairingSupported() {
		t.Fatal("PairingSupported() = true without transaction-backed queries, want false")
	}
}

type unavailableQianwenTestTxStarter struct{}

func (unavailableQianwenTestTxStarter) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected test transaction")
}

func TestMintPairingCodeRetriesInstallationCodeCollision(t *testing.T) {
	queries := authenticatedQueries(t)
	queries.pairingErrs = []error{
		&pgconn.PgError{Code: "23505", ConstraintName: "idx_qianwen_pairing_code_installation_code"},
		nil,
	}
	queries.pairingResult = db.QianwenPairingCode{ExpiresAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}
	service := mustTestService(t, queries, &fakeSessionEnsurer{}, &fakeTaskSender{})
	service.pairingRandom = bytes.NewReader([]byte{
		0, 0, 0, 1,
		0, 0, 0, 2,
	})

	result, err := service.MintPairingCode(
		context.Background(),
		queries.installation.WorkspaceID,
		queries.installation.ID,
		queries.installation.InstallerUserID,
	)
	if err != nil {
		t.Fatalf("MintPairingCode() error = %v", err)
	}
	if result.Code != "00000002" {
		t.Fatalf("MintPairingCode() code = %q, want collision retry code 00000002", result.Code)
	}
	if len(queries.pairingArgs) != 2 {
		t.Fatalf("UpsertQianwenPairingCode calls = %d, want 2", len(queries.pairingArgs))
	}
	if bytes.Equal(queries.pairingArgs[0].CodeDigest, queries.pairingArgs[1].CodeDigest) {
		t.Fatal("collision retry reused the first code digest")
	}
}

func authenticatedQueries(t *testing.T) *fakeServiceQueries {
	t.Helper()
	if !ValidCredentialShape(testConnectionID, testAccessToken) || !ValidCredentialShape(testConnectionID, testWrongAccessToken) {
		t.Fatal("test credentials must have the exact production credential shape")
	}
	config, err := encodeInstallConfig(testConnectionID, testAccessToken)
	if err != nil {
		t.Fatalf("encodeInstallConfig() error = %v", err)
	}
	installation := db.ChannelInstallation{
		ID:              testPGUUID(4),
		WorkspaceID:     testPGUUID(5),
		AgentID:         testPGUUID(2),
		InstallerUserID: testPGUUID(6),
		ChannelType:     string(TypeQianwen),
		Config:          config,
		Status:          "active",
	}
	return &fakeServiceQueries{
		installation:   installation,
		invocationUser: installation.InstallerUserID,
		claimResult: db.QianwenSkillRequest{
			InstallationID: installation.ID,
			RequestID:      util.MustParseUUID(testRequestID),
			MulticaUserID:  installation.InstallerUserID,
			ClaimToken:     testPGUUID(7),
		},
		setSessionRows: 1,
		releaseRows:    1,
		chatSession: db.ChatSession{
			ID:          testPGUUID(8),
			WorkspaceID: installation.WorkspaceID,
			AgentID:     installation.AgentID,
			CreatorID:   installation.InstallerUserID,
			Status:      "active",
		},
		agent: db.Agent{
			ID:          installation.AgentID,
			WorkspaceID: installation.WorkspaceID,
			OwnerID:     installation.InstallerUserID,
			RuntimeID:   testPGUUID(3),
		},
	}
}

func mustTestService(t *testing.T, queries serviceQueries, sessions RequestSessionEnsurer, tasks ChannelTaskSender) *Service {
	t.Helper()
	service, err := newService(queries, sessions, tasks, []byte("qianwen-unit-test-deployment-secret"))
	if err != nil {
		t.Fatalf("newService() error = %v", err)
	}
	// Public construction always supplies these transaction-backed dependencies.
	// Unit tests use interface fakes for the methods under test, so attach inert
	// sentinels solely to model a fully constructed production Service.
	service.dbq = &db.Queries{}
	service.tx = unavailableQianwenTestTxStarter{}
	return service
}

func signedUnitSubmitInvocation(service *Service, token string, request SubmitRequest) SubmitInvocation {
	invocation := SubmitInvocation{
		Request: request,
		Identity: InvocationMetadata{
			OpenUserID: "opaque-unit-user",
			OpenUUID:   "opaque-unit-device",
			Timestamp:  fmt.Sprint(service.now().UnixMilli()),
			Nonce:      "0123456789abcdef0123456789abcdef",
		},
	}
	canonical, err := CanonicalSubmitInvocation(invocation)
	if err != nil {
		// Invalid-request tests intentionally exercise malformed bodies. They
		// still need an envelope, but verification will reject it before any DB
		// call regardless of the placeholder signature.
		invocation.Identity.Signature = strings.Repeat("0", sha256.Size*2)
		return invocation
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	invocation.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return invocation
}

func signedUnitStatusInvocation(service *Service, token, requestID string) StatusInvocation {
	invocation := StatusInvocation{
		RequestID: requestID,
		Identity: InvocationMetadata{
			OpenUserID: "opaque-unit-user",
			OpenUUID:   "opaque-unit-device",
			Timestamp:  fmt.Sprint(service.now().UnixMilli()),
			Nonce:      "fedcba9876543210fedcba9876543210",
		},
	}
	canonical, err := CanonicalStatusInvocation(invocation)
	if err != nil {
		invocation.Identity.Signature = strings.Repeat("0", sha256.Size*2)
		return invocation
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	invocation.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return invocation
}

func queryDigest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return append([]byte(nil), sum[:]...)
}

func testPGUUID(first byte) pgtype.UUID {
	var value [16]byte
	value[0] = first
	return pgtype.UUID{Bytes: value, Valid: true}
}
