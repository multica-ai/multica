// Package secretrekey re-seals stored secretbox ciphertext under each
// integration's current key, so the key it replaced
// (MULTICA_*_SECRET_KEY_PREVIOUS) can be removed without breaking any
// connection.
//
// Every row is handled in its own short transaction: lock the row, skip it
// when it already opens under the current key, otherwise open it with the
// previous key, seal it with the current key and write back only that value.
// A row that opens under neither key is reported and left untouched.
package secretrekey

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// Encoding is how a sealed value is stored.
type Encoding int

const (
	// Raw stores nonce||ciphertext||tag in a BYTEA column.
	Raw Encoding = iota
	// Base64 stores standard base64 in a TEXT column or a JSONB string field.
	// Reads tolerate the newline wrapping PostgreSQL's encode(..., 'base64')
	// emits; writes are unwrapped, matching the application writers.
	Base64
)

// Target is one stored secret: a column, or a string field inside a JSONB
// column, of rows matching Where.
type Target struct {
	// KeyEnv is the env var of the key that seals this value.
	KeyEnv string
	Table  string
	// KeyColumns identify a row. They are compared as text.
	KeyColumns []string
	Column     string
	// JSONField, when set, names the string field inside the JSONB Column.
	JSONField string
	// Where is an optional extra SQL filter, such as a channel type.
	Where    string
	Encoding Encoding
	// Optional skips the target when the table does not exist.
	Optional bool
}

// Name labels the target in logs and reports.
func (t Target) Name() string {
	name := t.Table + "." + t.Column
	if t.JSONField != "" {
		name += "->" + t.JSONField
	}
	if t.Where != "" {
		name += " [" + t.Where + "]"
	}
	return name
}

func channelConfig(env, channelType, field string) Target {
	return Target{
		KeyEnv: env, Table: "channel_installation", KeyColumns: []string{"id"},
		Column: "config", JSONField: field, Where: "channel_type = '" + channelType + "'", Encoding: Base64,
	}
}

// Targets lists every stored value sealed with an integration key.
// MULTICA_PLUGIN_SECRET_KEY is not covered.
var Targets = []Target{
	// Feishu / Lark
	channelConfig("MULTICA_LARK_SECRET_KEY", "feishu", "app_secret_encrypted"),
	{KeyEnv: "MULTICA_LARK_SECRET_KEY", Table: "channel_typing_reaction", KeyColumns: []string{"id"}, Column: "installation_snapshot", JSONField: "AppSecretEncrypted", Encoding: Base64},
	// Pre-generalization table, kept after its rows were copied into
	// channel_installation.
	{KeyEnv: "MULTICA_LARK_SECRET_KEY", Table: "lark_installation", KeyColumns: []string{"id"}, Column: "app_secret_encrypted", Encoding: Raw, Optional: true},

	// Slack
	channelConfig("MULTICA_SLACK_SECRET_KEY", "slack", "bot_token_encrypted"),
	channelConfig("MULTICA_SLACK_SECRET_KEY", "slack", "app_token_encrypted"),

	// DingTalk
	channelConfig("MULTICA_DINGTALK_SECRET_KEY", "dingtalk", "app_secret_encrypted"),

	// WeCom
	channelConfig("MULTICA_WECOM_SECRET_KEY", "wecom", "secret_encrypted"),
	{KeyEnv: "MULTICA_WECOM_SECRET_KEY", Table: "wecom_install_session", KeyColumns: []string{"id"}, Column: "scode_encrypted", Encoding: Base64},
	{KeyEnv: "MULTICA_WECOM_SECRET_KEY", Table: "wecom_install_session", KeyColumns: []string{"id"}, Column: "qr_code_url_encrypted", Encoding: Base64},

	// WeChat
	channelConfig("MULTICA_WEIXIN_SECRET_KEY", "weixin", "bot_token_encrypted"),
	{KeyEnv: "MULTICA_WEIXIN_SECRET_KEY", Table: "weixin_account_state", KeyColumns: []string{"installation_id"}, Column: "context_token_encrypted", Encoding: Raw},
	{KeyEnv: "MULTICA_WEIXIN_SECRET_KEY", Table: "weixin_login_session", KeyColumns: []string{"id"}, Column: "state_encrypted", Encoding: Raw},

	// Telegram
	channelConfig("MULTICA_TELEGRAM_SECRET_KEY", "telegram", "bot_token_encrypted"),

	// Connected Apps
	{KeyEnv: "MULTICA_APPS_SECRET_KEY", Table: "remote_app_oauth_client", KeyColumns: []string{"id"}, Column: "client_secret", Encoding: Raw},
	{KeyEnv: "MULTICA_APPS_SECRET_KEY", Table: "remote_app_connection", KeyColumns: []string{"id"}, Column: "access_token", Encoding: Raw},
	{KeyEnv: "MULTICA_APPS_SECRET_KEY", Table: "remote_app_connection", KeyColumns: []string{"id"}, Column: "refresh_token", Encoding: Raw},

	// VCS and cloud agents
	{KeyEnv: "MULTICA_VCS_SECRET_KEY", Table: "vcs_connection", KeyColumns: []string{"id"}, Column: "access_token_encrypted", Encoding: Base64},
	{KeyEnv: "MULTICA_VCS_SECRET_KEY", Table: "vcs_connection", KeyColumns: []string{"id"}, Column: "webhook_secret_encrypted", Encoding: Base64},
	{KeyEnv: "MULTICA_VCS_SECRET_KEY", Table: "cloud_agent_connection", KeyColumns: []string{"workspace_id", "provider"}, Column: "config_encrypted", Encoding: Base64},
	{KeyEnv: "MULTICA_VCS_SECRET_KEY", Table: "cloud_agent_connection", KeyColumns: []string{"workspace_id", "provider"}, Column: "journal_encrypted", Encoding: Base64},
}

// Report counts the non-empty values of one target.
type Report struct {
	Target Target
	// Skipped explains why the target was not scanned (no key configured,
	// table absent); counts are zero when set.
	Skipped string
	// Current values already open under the current key.
	Current int
	// Previous values were sealed under a previous key: re-sealed when
	// applying, still pending otherwise.
	Previous int
	// Unreadable values open under no configured key and are left untouched.
	Unreadable int
	// UnreadableKeys identify the unreadable rows, as text key tuples.
	UnreadableKeys [][]string
}

// Run scans every target whose key env is in boxes. With apply, values
// sealed under a previous key are re-sealed under the current key.
func Run(ctx context.Context, pool *pgxpool.Pool, targets []Target, boxes map[string]*secretbox.Box, apply bool) ([]Report, error) {
	reports := make([]Report, 0, len(targets))
	for _, t := range targets {
		r, err := runTarget(ctx, pool, t, boxes[t.KeyEnv], apply)
		if err != nil {
			return reports, fmt.Errorf("%s: %w", t.Name(), err)
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func runTarget(ctx context.Context, pool *pgxpool.Pool, t Target, box *secretbox.Box, apply bool) (Report, error) {
	r := Report{Target: t}
	if box == nil {
		r.Skipped = t.KeyEnv + " not set"
		return r, nil
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, t.Table).Scan(&exists); err != nil {
		return r, err
	}
	if !exists {
		if t.Optional {
			r.Skipped = "table absent"
			return r, nil
		}
		return r, fmt.Errorf("table %s does not exist", t.Table)
	}

	keys, err := listKeys(ctx, pool, t)
	if err != nil {
		return r, err
	}
	for _, key := range keys {
		outcome, err := processRow(ctx, pool, t, box, key, apply)
		if err != nil {
			return r, fmt.Errorf("row %v: %w", key, err)
		}
		switch outcome {
		case outcomeCurrent:
			r.Current++
		case outcomePrevious:
			r.Previous++
		case outcomeUnreadable:
			r.Unreadable++
			r.UnreadableKeys = append(r.UnreadableKeys, key)
		}
	}
	return r, nil
}

type outcome int

const (
	outcomeEmpty outcome = iota
	outcomeCurrent
	outcomePrevious
	outcomeUnreadable
)

func (t Target) valueExpr() string {
	if t.JSONField != "" {
		return fmt.Sprintf("(%s ->> '%s')", pgx.Identifier{t.Column}.Sanitize(), t.JSONField)
	}
	return pgx.Identifier{t.Column}.Sanitize()
}

func (t Target) filter() string {
	parts := []string{t.valueExpr() + " IS NOT NULL"}
	if t.Where != "" {
		parts = append(parts, "("+t.Where+")")
	}
	return strings.Join(parts, " AND ")
}

func (t Target) keyMatch(offset int) string {
	parts := make([]string, len(t.KeyColumns))
	for i, c := range t.KeyColumns {
		parts[i] = fmt.Sprintf("%s::text = $%d", pgx.Identifier{c}.Sanitize(), offset+i)
	}
	return strings.Join(parts, " AND ")
}

func listKeys(ctx context.Context, pool *pgxpool.Pool, t Target) ([][]string, error) {
	cols := make([]string, len(t.KeyColumns))
	for i, c := range t.KeyColumns {
		cols[i] = pgx.Identifier{c}.Sanitize() + "::text"
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY 1`,
		strings.Join(cols, ", "), pgx.Identifier{t.Table}.Sanitize(), t.filter()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys [][]string
	for rows.Next() {
		key := make([]string, len(t.KeyColumns))
		dest := make([]any, len(key))
		for i := range key {
			dest[i] = &key[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func processRow(ctx context.Context, pool *pgxpool.Pool, t Target, box *secretbox.Box, key []string, apply bool) (outcome, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	args := make([]any, len(key))
	for i, k := range key {
		args[i] = k
	}
	lock := ""
	if apply {
		lock = " FOR UPDATE"
	}
	var stored any
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE %s AND %s%s`,
		t.valueExpr(), pgx.Identifier{t.Table}.Sanitize(), t.keyMatch(1), t.filter(), lock), args...).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted or cleared since listing.
		return outcomeEmpty, nil
	}
	if err != nil {
		return 0, err
	}

	sealed, ok := decode(t.Encoding, stored)
	if !ok {
		return outcomeUnreadable, nil
	}
	if len(sealed) == 0 {
		return outcomeEmpty, nil
	}
	if box.SealedWithCurrent(sealed) {
		return outcomeCurrent, nil
	}
	plaintext, err := box.Open(sealed)
	if err != nil {
		return outcomeUnreadable, nil
	}
	if !apply {
		return outcomePrevious, nil
	}

	resealed, err := box.Seal(plaintext)
	if err != nil {
		return 0, err
	}
	var value any = resealed
	set := fmt.Sprintf("%s = $1", pgx.Identifier{t.Column}.Sanitize())
	if t.Encoding == Base64 {
		value = base64.StdEncoding.EncodeToString(resealed)
	}
	if t.JSONField != "" {
		col := pgx.Identifier{t.Column}.Sanitize()
		set = fmt.Sprintf("%s = jsonb_set(%s, ARRAY['%s'], to_jsonb($1::text))", col, col, t.JSONField)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE %s`,
		pgx.Identifier{t.Table}.Sanitize(), set, t.keyMatch(2)), append([]any{value}, args...)...); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return outcomePrevious, nil
}

func decode(enc Encoding, stored any) ([]byte, bool) {
	switch v := stored.(type) {
	case nil:
		return nil, true
	case []byte:
		if enc == Raw {
			return v, true
		}
		return decodeBase64(string(v))
	case string:
		if enc == Raw {
			return nil, false
		}
		return decodeBase64(v)
	default:
		return nil, false
	}
}

func decodeBase64(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
	b, err := base64.StdEncoding.DecodeString(s)
	return b, err == nil
}
