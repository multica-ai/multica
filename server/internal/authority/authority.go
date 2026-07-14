package authority

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const ProtocolVersion = "multica-authority-attestation-v1"

type DBIdentity struct {
	SystemIdentifier string `json:"system_identifier"`
	DatabaseOID      int64  `json:"database_oid"`
	DatabaseName     string `json:"database_name"`
}

type Statement struct {
	Protocol     string
	Nonce        string
	AuthorityID  string
	DBIdentity   DBIdentity
	IssuedAt     time.Time
	ServerCommit string
}

type Attestation struct {
	Protocol     string     `json:"protocol"`
	Nonce        string     `json:"nonce"`
	AuthorityID  string     `json:"authority_id"`
	DBIdentity   DBIdentity `json:"db_identity"`
	IssuedAt     string     `json:"issued_at"`
	ServerCommit string     `json:"server_commit"`
	Signature    string     `json:"signature"`
}

type Pin struct {
	ServerURL   string     `json:"server_url,omitempty"`
	PublicKey   string     `json:"public_key,omitempty"`
	AuthorityID string     `json:"authority_id,omitempty"`
	DBIdentity  DBIdentity `json:"db_identity,omitempty"`
}

func GenerateNonce(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ValidateNonce(nonce string) ([]byte, error) {
	if nonce == "" {
		return nil, errors.New("nonce is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil {
		return nil, errors.New("nonce must be unpadded base64url")
	}
	if len(raw) != 32 {
		return nil, errors.New("nonce must decode to exactly 32 bytes")
	}
	if base64.RawURLEncoding.EncodeToString(raw) != nonce {
		return nil, errors.New("nonce must be canonical unpadded base64url")
	}
	return raw, nil
}

func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(pub)
}

func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("public key must be unpadded base64url")
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("public key must decode to 32 bytes")
	}
	if base64.RawURLEncoding.EncodeToString(raw) != s {
		return nil, errors.New("public key must be canonical unpadded base64url")
	}
	return ed25519.PublicKey(raw), nil
}

func Sign(priv ed25519.PrivateKey, stmt Statement) (Attestation, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return Attestation{}, errors.New("private key must be Ed25519")
	}
	payload, err := CanonicalPayload(stmt)
	if err != nil {
		return Attestation{}, err
	}
	return Attestation{
		Protocol:     stmt.Protocol,
		Nonce:        stmt.Nonce,
		AuthorityID:  stmt.AuthorityID,
		DBIdentity:   stmt.DBIdentity,
		IssuedAt:     stmt.IssuedAt.UTC().Format(time.RFC3339Nano),
		ServerCommit: stmt.ServerCommit,
		Signature:    base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, payload)),
	}, nil
}

func Verify(att Attestation, pin Pin, serverURL string, now time.Time, maxAge, futureSkew time.Duration) error {
	if att.Protocol != ProtocolVersion {
		return errors.New("unexpected protocol")
	}
	if _, err := ValidateNonce(att.Nonce); err != nil {
		return fmt.Errorf("invalid nonce: %w", err)
	}
	if pin.AuthorityID == "" || att.AuthorityID != pin.AuthorityID {
		return errors.New("authority id does not match pin")
	}
	if att.DBIdentity != pin.DBIdentity {
		return errors.New("database identity does not match pin")
	}
	normalizedPin, err := NormalizeServerURL(pin.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid pinned server url: %w", err)
	}
	normalizedServer, err := NormalizeServerURL(serverURL)
	if err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}
	if normalizedPin != normalizedServer {
		return errors.New("server url does not match authority pin")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, att.IssuedAt)
	if err != nil {
		return errors.New("issued_at must be RFC3339Nano")
	}
	if issuedAt.UTC().Format(time.RFC3339Nano) != att.IssuedAt {
		return errors.New("issued_at must use canonical UTC RFC3339Nano encoding")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if futureSkew < 0 || maxAge <= 0 {
		return errors.New("invalid freshness bounds")
	}
	if issuedAt.After(now.Add(futureSkew)) {
		return errors.New("attestation issued_at is in the future")
	}
	if now.Sub(issuedAt) > maxAge {
		return errors.New("attestation is stale")
	}

	pub, err := DecodePublicKey(pin.PublicKey)
	if err != nil {
		return err
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(att.Signature)
	if err != nil {
		return errors.New("signature must be strict unpadded base64url")
	}
	if len(sig) != ed25519.SignatureSize {
		return errors.New("signature must decode to 64 bytes")
	}
	if base64.RawURLEncoding.EncodeToString(sig) != att.Signature {
		return errors.New("signature must use canonical unpadded base64url encoding")
	}
	stmt := Statement{
		Protocol:     att.Protocol,
		Nonce:        att.Nonce,
		AuthorityID:  att.AuthorityID,
		DBIdentity:   att.DBIdentity,
		IssuedAt:     issuedAt,
		ServerCommit: att.ServerCommit,
	}
	payload, err := CanonicalPayload(stmt)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}

func CanonicalPayload(stmt Statement) ([]byte, error) {
	if stmt.Protocol != ProtocolVersion {
		return nil, errors.New("unsupported protocol")
	}
	if _, err := ValidateNonce(stmt.Nonce); err != nil {
		return nil, err
	}
	if err := validateAuthorityID(stmt.AuthorityID); err != nil {
		return nil, err
	}
	if err := validateDBIdentity(stmt.DBIdentity); err != nil {
		return nil, err
	}
	if stmt.IssuedAt.IsZero() {
		return nil, errors.New("issued_at is required")
	}
	if strings.TrimSpace(stmt.ServerCommit) == "" {
		return nil, errors.New("server commit is required")
	}

	var b []byte
	b = append(b, []byte("multica-authority-attestation-signed-payload")...)
	b = append(b, 0)
	appendField := func(name, value string) {
		var lenbuf [4]byte
		binary.BigEndian.PutUint32(lenbuf[:], uint32(len(name)))
		b = append(b, lenbuf[:]...)
		b = append(b, name...)
		binary.BigEndian.PutUint32(lenbuf[:], uint32(len(value)))
		b = append(b, lenbuf[:]...)
		b = append(b, value...)
	}
	appendField("protocol", stmt.Protocol)
	appendField("nonce", stmt.Nonce)
	appendField("authority_id", stmt.AuthorityID)
	appendField("db_system_identifier", stmt.DBIdentity.SystemIdentifier)
	appendField("db_oid", fmt.Sprintf("%d", stmt.DBIdentity.DatabaseOID))
	appendField("db_name", stmt.DBIdentity.DatabaseName)
	appendField("issued_at", stmt.IssuedAt.UTC().Format(time.RFC3339Nano))
	appendField("server_commit", stmt.ServerCommit)
	return b, nil
}

func NormalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("server url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("server url must use http or https")
	}
	if u.Host == "" {
		return "", errors.New("server url must include host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("server url must not include query or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.EscapedPath(), "/")
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

func validateAuthorityID(id string) error {
	if id == "" {
		return errors.New("authority id is required")
	}
	if len(id) > 128 {
		return errors.New("authority id is too long")
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return errors.New("authority id contains invalid characters")
	}
	return nil
}

func validateDBIdentity(id DBIdentity) error {
	if id.SystemIdentifier == "" {
		return errors.New("database system identifier is required")
	}
	for _, r := range id.SystemIdentifier {
		if r < '0' || r > '9' {
			return errors.New("database system identifier must be decimal text")
		}
	}
	if id.DatabaseOID <= 0 {
		return errors.New("database oid is required")
	}
	if id.DatabaseName == "" {
		return errors.New("database name is required")
	}
	return nil
}
