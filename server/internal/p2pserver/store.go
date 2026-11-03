package p2pserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrPeerTaken means another account already registered the public key.
var ErrPeerTaken = errors.New("p2pserver: peer id belongs to another account")

// Device is one daemon profile's link key.
type Device struct {
	UserID     string `json:"-"`
	DaemonID   string `json:"daemon_id"`
	Profile    string `json:"profile"`
	PeerID     string `json:"peer_id"`
	PublicKey  string `json:"public_key"`
	DeviceName string `json:"device_name"`
	// Addrs are the multiaddrs the device last reported, including relay
	// circuit addresses.
	Addrs     []string  `json:"addrs"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store persists devices and sessions in a single SQLite file.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS device (
    user_id     TEXT NOT NULL,
    daemon_id   TEXT NOT NULL,
    profile     TEXT NOT NULL,
    peer_id     TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    device_name TEXT NOT NULL,
    addrs       TEXT NOT NULL DEFAULT '[]',
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, daemon_id, profile)
);
CREATE INDEX IF NOT EXISTS device_updated_at ON device (updated_at);
CREATE UNIQUE INDEX IF NOT EXISTS device_peer_id ON device (peer_id);

CREATE TABLE IF NOT EXISTS session (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS session_expires_at ON session (expires_at);
`

// OpenStore opens or creates the database at path.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// SQLite serializes writers; one connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("p2pserver: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks that the database answers queries.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// Upsert registers or refreshes a device. A key is one device: registering
// it under another daemon or profile of the same account moves it there,
// while a key owned by another account is refused with ErrPeerTaken. It reports
// whether the account's other devices would see anything different: a refresh of
// an unchanged, unexpired device (one updated at or after liveSince) is not news.
func (s *Store) Upsert(ctx context.Context, d Device, now, liveSince time.Time) (Device, bool, error) {
	d.UpdatedAt = now.UTC().Truncate(time.Millisecond)
	if d.Addrs == nil {
		d.Addrs = []string{}
	}
	addrs, err := json.Marshal(d.Addrs)
	if err != nil {
		return Device{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, false, err
	}
	defer tx.Rollback()

	var owner, daemonID, profile, deviceName, previousAddrs string
	var updated int64
	changed := true
	err = tx.QueryRowContext(ctx, `SELECT user_id, daemon_id, profile, device_name, addrs, updated_at FROM device WHERE peer_id = ?`, d.PeerID).Scan(&owner, &daemonID, &profile, &deviceName, &previousAddrs, &updated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Device{}, false, err
	case owner != d.UserID:
		return Device{}, false, ErrPeerTaken
	case daemonID != d.DaemonID || profile != d.Profile:
		if _, err := tx.ExecContext(ctx, `DELETE FROM device WHERE peer_id = ?`, d.PeerID); err != nil {
			return Device{}, false, err
		}
	default:
		changed = deviceName != d.DeviceName || previousAddrs != string(addrs) || updated < liveSince.UnixMilli()
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO device (user_id, daemon_id, profile, peer_id, public_key, device_name, addrs, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, daemon_id, profile) DO UPDATE SET
    peer_id = excluded.peer_id,
    public_key = excluded.public_key,
    device_name = excluded.device_name,
    addrs = excluded.addrs,
    updated_at = excluded.updated_at`,
		d.UserID, d.DaemonID, d.Profile, d.PeerID, d.PublicKey, d.DeviceName, string(addrs), d.UpdatedAt.UnixMilli())
	if err != nil {
		return Device{}, false, err
	}
	return d, changed, tx.Commit()
}

// List returns the account's devices refreshed at or after since.
func (s *Store) List(ctx context.Context, userID string, since time.Time) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT daemon_id, profile, peer_id, public_key, device_name, addrs, updated_at
FROM device WHERE user_id = ? AND updated_at >= ?
ORDER BY daemon_id, profile`, userID, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d := Device{UserID: userID}
		var addrs string
		var updated int64
		if err := rows.Scan(&d.DaemonID, &d.Profile, &d.PeerID, &d.PublicKey, &d.DeviceName, &addrs, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(addrs), &d.Addrs); err != nil || d.Addrs == nil {
			d.Addrs = []string{}
		}
		d.UpdatedAt = time.UnixMilli(updated).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// PeerOwner returns the account a peer ID is registered to, if its device
// was refreshed at or after since.
func (s *Store) PeerOwner(ctx context.Context, peerID string, since time.Time) (string, bool, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM device WHERE peer_id = ? AND updated_at >= ?`, peerID, since.UnixMilli()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return userID, err == nil, err
}

// Delete removes one device and returns its peer ID, or "" if it did not
// exist.
func (s *Store) Delete(ctx context.Context, userID, daemonID, profile string) (string, error) {
	var peerID string
	err := s.db.QueryRowContext(ctx, `DELETE FROM device WHERE user_id = ? AND daemon_id = ? AND profile = ? RETURNING peer_id`, userID, daemonID, profile).Scan(&peerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return peerID, err
}

// PurgeBefore deletes devices not refreshed since cutoff and sessions
// expired by now, returning the number of devices removed.
func (s *Store) PurgeBefore(ctx context.Context, cutoff, now time.Time) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE expires_at <= ?`, now.UnixMilli()); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM device WHERE updated_at < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CreateSession issues a session token for the account. Only its hash is
// stored.
func (s *Store) CreateSession(ctx context.Context, userID string, expires time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx, `INSERT INTO session (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expires.UnixMilli())
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionUser resolves a session token that has not expired by now.
func (s *Store) SessionUser(ctx context.Context, token string, now time.Time) (string, bool, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM session WHERE token_hash = ? AND expires_at > ?`, hashToken(token), now.UnixMilli()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return userID, err == nil, err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
