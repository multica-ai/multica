package runtime

// api_connection_session_exchange.go (FIR-2564 fase 2): per-person session keys
// for API-type connections.
//
// A connection with auth_config.session_exchange.enabled no longer calls the
// remote API with its one shared key when a triggering human is known. Instead
// the backend first EXCHANGES the shared key for that person's own short-lived
// session key (the Firtal Data Registry contract: POST <url>/sessions/exchange
// with body {"principal": "member:<uuid>", "ttlSeconds": n} → 201 {"key",
// "expires_at"}), caches the key encrypted in cerebro_connection_person_key,
// and dispatches the data call on the personal key. The remote API then
// enforces THAT person's access, and revoking one person's key stops exactly
// that person.
//
// Trust and failure posture:
//   - The person is ONLY the triggering human (GatewayRequestMeta.TriggerUserID
//     via WithConnectionTriggerMember). The agent's owner is never substituted —
//     a run triggered by X must not borrow Y's access.
//   - No triggering human (system runs, local connection-tools surface) → the
//     call proceeds on the shared key, exactly as before.
//   - Exchange enabled + triggering human known + exchange fails → the call
//     FAILS (closed). Falling back to the shared key would silently answer with
//     the wrong (broader) access.
//   - Exchanged keys are cached encrypted (MULTICA_CREDENTIALS_KEY, AES-256-GCM
//     via internal/cerebro/credentials) and reused until shortly before expiry.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	// sessionExchangeDefaultPath is the exchange endpoint relative to the
	// connection URL when auth_config.session_exchange.path is unset.
	sessionExchangeDefaultPath = "/sessions/exchange"
	// sessionExchangeDefaultTTLSeconds is the requested key lifetime when
	// auth_config.session_exchange.ttl_seconds is unset. The remote API caps it.
	sessionExchangeDefaultTTLSeconds = 3600
	// sessionExchangeExpirySkew is how long before expiry a cached key stops
	// being served, so an in-flight call never rides a key that expires mid-call.
	sessionExchangeExpirySkew = 60 * time.Second
	// sessionExchangeResponseLimit bounds the exchange response read.
	sessionExchangeResponseLimit = 64 << 10 // 64 KiB
)

// connectionTriggerMemberKey carries the triggering human's member UUID from
// the gateway tool loop down to APIConnectionTool.Call via context.
type connectionTriggerMemberKey struct{}

// WithConnectionTriggerMember returns a context carrying the triggering
// human's member id for connection dispatch. An empty id returns ctx unchanged.
func WithConnectionTriggerMember(ctx context.Context, memberID string) context.Context {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return ctx
	}
	return context.WithValue(ctx, connectionTriggerMemberKey{}, memberID)
}

// ConnectionTriggerMember returns the triggering human's member id carried by
// the context, or "" when the run has no human origin.
func ConnectionTriggerMember(ctx context.Context) string {
	s, _ := ctx.Value(connectionTriggerMemberKey{}).(string)
	return s
}

// connectionPersonKeyStore is the slice of cerebrodb.Queries the exchanger
// needs; *cerebrodb.Queries satisfies it after sqlc generation.
type connectionPersonKeyStore interface {
	GetConnectionPersonKey(ctx context.Context, arg cerebrodb.GetConnectionPersonKeyParams) (cerebrodb.CerebroConnectionPersonKey, error)
	UpsertConnectionPersonKey(ctx context.Context, arg cerebrodb.UpsertConnectionPersonKeyParams) (cerebrodb.CerebroConnectionPersonKey, error)
}

// ConnectionSessionExchanger exchanges a connection's shared key for a
// person's own session key and caches it encrypted, one row per
// (connection, member).
type ConnectionSessionExchanger struct {
	store     connectionPersonKeyStore
	cipher    *credentials.Cipher
	cipherErr error
	logger    *slog.Logger
	now       func() time.Time
}

// NewConnectionSessionExchanger builds an exchanger. The cipher comes from
// MULTICA_CREDENTIALS_KEY; when that is unset the exchanger still constructs
// but every exchange fails closed with a clear error.
func NewConnectionSessionExchanger(store connectionPersonKeyStore, logger *slog.Logger) *ConnectionSessionExchanger {
	if logger == nil {
		logger = slog.Default()
	}
	cipher, err := credentials.NewCipherFromEnv()
	if cipher == nil && err == nil {
		// NewCipherFromEnv returns (nil, nil) when the env var is unset.
		err = fmt.Errorf("%s is not set", credentials.KeyEnvVar)
	}
	if err != nil {
		logger.Warn("connection session exchange: credentials cipher unavailable — exchanges will fail closed", "error", err)
	}
	return &ConnectionSessionExchanger{
		store:     store,
		cipher:    cipher,
		cipherErr: err,
		logger:    logger,
		now:       time.Now,
	}
}

// exchangeResponse is the subset of the registry's 201 payload we consume.
type exchangeResponse struct {
	Key       string `json:"key"`
	ExpiresAt string `json:"expires_at"`
}

// PersonalKey returns a valid session key for memberID on the given
// connection, reusing the encrypted cache when the cached key is still valid
// past the expiry skew, otherwise exchanging the connection's shared key for
// a fresh one. Fail closed: any error means the data call must not proceed.
func (x *ConnectionSessionExchanger) PersonalKey(
	ctx context.Context,
	client *http.Client,
	workspaceID, connectionID, baseURL string,
	auth connections.AuthConfig,
	memberID string,
) (string, error) {
	if x == nil || x.store == nil {
		return "", fmt.Errorf("session exchange is not wired on this surface")
	}
	if x.cipher == nil {
		return "", fmt.Errorf("session exchange unavailable: %w", x.cipherErr)
	}
	connUUID, err := util.ParseUUID(connectionID)
	if err != nil {
		return "", fmt.Errorf("session exchange: invalid connection id: %w", err)
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return "", fmt.Errorf("session exchange: invalid workspace id: %w", err)
	}
	memberUUID, err := util.ParseUUID(memberID)
	if err != nil {
		return "", fmt.Errorf("session exchange: invalid member id: %w", err)
	}

	now := x.now()
	cached, err := x.store.GetConnectionPersonKey(ctx, cerebrodb.GetConnectionPersonKeyParams{
		ConnectionID: connUUID,
		MemberID:     memberUUID,
	})
	if err == nil && cached.ExpiresAt.Valid && cached.ExpiresAt.Time.After(now.Add(sessionExchangeExpirySkew)) {
		plain, derr := x.cipher.Decrypt(cached.KeyCiphertext)
		if derr == nil {
			return string(plain), nil
		}
		// A corrupt/undecryptable cache row is replaced by a fresh exchange.
		x.logger.Warn("connection session exchange: cached key decrypt failed — re-exchanging",
			"connection_id", connectionID, "member_id", memberID, "error", derr)
	}

	key, expiresAt, err := x.exchange(ctx, client, baseURL, auth, memberID)
	if err != nil {
		return "", err
	}

	ciphertext, err := x.cipher.Encrypt([]byte(key))
	if err != nil {
		return "", fmt.Errorf("session exchange: encrypt key: %w", err)
	}
	if _, err := x.store.UpsertConnectionPersonKey(ctx, cerebrodb.UpsertConnectionPersonKeyParams{
		WorkspaceID:   wsUUID,
		ConnectionID:  connUUID,
		MemberID:      memberUUID,
		KeyCiphertext: ciphertext,
		ExpiresAt:     pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		// The key itself is valid; a cache write failure only costs a re-exchange
		// on the next call. Log and continue.
		x.logger.Warn("connection session exchange: cache write failed",
			"connection_id", connectionID, "member_id", memberID, "error", err)
	}
	x.logger.Info("connection session exchange: issued personal key",
		"connection_id", connectionID, "member_id", memberID, "expires_at", expiresAt.Format(time.RFC3339))
	return key, nil
}

// exchange performs the POST <baseURL><path> call with the connection's shared
// credentials and returns the personal key + expiry.
func (x *ConnectionSessionExchanger) exchange(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	auth connections.AuthConfig,
	memberID string,
) (string, time.Time, error) {
	se := auth.SessionExchange
	path := sessionExchangeDefaultPath
	ttl := sessionExchangeDefaultTTLSeconds
	if se != nil {
		if strings.TrimSpace(se.Path) != "" {
			path = "/" + strings.TrimLeft(strings.TrimSpace(se.Path), "/")
		}
		if se.TTLSeconds > 0 {
			ttl = se.TTLSeconds
		}
	}

	payload, err := json.Marshal(map[string]any{
		"principal":  "member:" + memberID,
		"ttlSeconds": ttl,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session exchange: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session exchange: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyConnectionAuth(req, auth)

	if client == nil {
		client = &http.Client{Timeout: apiConnectionToolTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session exchange: call failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, sessionExchangeResponseLimit))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session exchange: read response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// The body may quote the shared credential on misconfigured endpoints;
		// redact before surfacing.
		return "", time.Time{}, fmt.Errorf("session exchange: HTTP %d: %s",
			resp.StatusCode, redactCredentials(strings.TrimSpace(string(body)), auth))
	}
	var parsed exchangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", time.Time{}, fmt.Errorf("session exchange: parse response: %w", err)
	}
	if parsed.Key == "" {
		return "", time.Time{}, fmt.Errorf("session exchange: response carried no key")
	}
	expiresAt, err := time.Parse(time.RFC3339, parsed.ExpiresAt)
	if err != nil {
		// No usable expiry: keep the key for this call only (cache with an
		// already-skewed expiry so the next call re-exchanges).
		expiresAt = x.now().Add(sessionExchangeExpirySkew)
	}
	return parsed.Key, expiresAt, nil
}

// personalKeyAuth returns the auth config to use for a data call running on a
// person's own key: the personal key takes the slot the shared key used
// (API-key header when configured, bearer otherwise), and the non-identity
// Cloudflare Access service-token pair is kept as-is.
func personalKeyAuth(auth connections.AuthConfig, key string) connections.AuthConfig {
	out := auth
	if auth.APIKey != "" || auth.BearerToken == "" {
		out.APIKey = key
		out.BearerToken = ""
	} else {
		out.BearerToken = key
		out.APIKey = ""
	}
	return out
}
