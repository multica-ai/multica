package authority

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
)

const maxPrivateKeyFileBytes = 16 * 1024

type Signer struct {
	AuthorityID string
	PrivateKey  ed25519.PrivateKey
	PublicKey   ed25519.PublicKey
}

func LoadSignerFromEnv(getenv func(string) string) (*Signer, error) {
	return loadSignerFromEnv(getenv, "")
}

func loadSignerFromEnv(getenv func(string) string, _ string) (*Signer, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	authorityID := strings.TrimSpace(getenv("MULTICA_AUTHORITY_ID"))
	keyFile := strings.TrimSpace(getenv("MULTICA_AUTHORITY_PRIVATE_KEY_FILE"))
	if authorityID == "" && keyFile == "" {
		return nil, nil
	}
	if authorityID == "" || keyFile == "" {
		return nil, errors.New("MULTICA_AUTHORITY_ID and MULTICA_AUTHORITY_PRIVATE_KEY_FILE must be configured together")
	}
	if err := validateAuthorityID(authorityID); err != nil {
		return nil, fmt.Errorf("invalid MULTICA_AUTHORITY_ID: %w", err)
	}
	priv, err := loadPrivateKeyFile(keyFile)
	if err != nil {
		return nil, err
	}
	return &Signer{
		AuthorityID: authorityID,
		PrivateKey:  priv,
		PublicKey:   priv.Public().(ed25519.PublicKey),
	}, nil
}

func loadPrivateKeyFile(path string) (ed25519.PrivateKey, error) {
	return loadPrivateKeyFileWithOpener(path, openPrivateKeyFileNoFollow)
}

func loadPrivateKeyFileWithOpener(path string, opener func(string) (*os.File, error)) (ed25519.PrivateKey, error) {
	if opener == nil {
		return nil, errors.New("authority private key file opener is unavailable")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat authority private key file: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("authority private key file must not be a symlink")
	}
	if err := validatePrivateKeyFileMetadata(pathInfo, os.Geteuid()); err != nil {
		return nil, err
	}
	file, err := opener(path)
	if err != nil {
		return nil, fmt.Errorf("open authority private key file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat opened authority private key file: %w", err)
	}
	if err := validatePrivateKeyFileMetadata(info, os.Geteuid()); err != nil {
		return nil, err
	}
	if !os.SameFile(pathInfo, info) {
		return nil, errors.New("authority private key file changed while opening")
	}
	if info.Size() <= 0 || info.Size() > maxPrivateKeyFileBytes {
		return nil, errors.New("authority private key file size is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPrivateKeyFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read authority private key file: %w", err)
	}
	if len(data) > maxPrivateKeyFileBytes {
		return nil, errors.New("authority private key file is too large")
	}
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, errors.New("authority private key file must contain PKCS#8 PEM")
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("authority private key file must contain exactly one PEM block")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, errors.New("authority private key PEM block must be PRIVATE KEY")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse authority private key: %w", err)
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("authority private key must be Ed25519 PKCS#8")
	}
	return priv, nil
}

func validatePrivateKeyFileMetadata(info os.FileInfo, expectedUID int) error {
	if info == nil || !info.Mode().IsRegular() {
		return errors.New("authority private key file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("authority private key file permissions must be 0600 or stricter")
	}
	ownerUID, err := fileOwnerUID(info)
	if err != nil {
		return fmt.Errorf("determine authority private key file owner: %w", err)
	}
	if expectedUID < 0 || ownerUID != expectedUID {
		return fmt.Errorf("authority private key file must be owned by effective uid %d", expectedUID)
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (int, error) {
	v := reflect.ValueOf(info.Sys())
	if !v.IsValid() {
		return 0, errors.New("file ownership metadata is unavailable")
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, errors.New("file ownership metadata is unavailable")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, errors.New("file ownership metadata has unexpected type")
	}
	uid := v.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return 0, errors.New("file ownership uid is unavailable")
	}
	return int(uid.Uint()), nil
}

func (s *Signer) Sign(stmt Statement) (Attestation, error) {
	if s == nil {
		return Attestation{}, errors.New("authority signer is not configured")
	}
	if stmt.AuthorityID == "" {
		stmt.AuthorityID = s.AuthorityID
	}
	return Sign(s.PrivateKey, stmt)
}
