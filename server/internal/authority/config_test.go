package authority

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writePKCS8KeyFile(t *testing.T, dir string, mode os.FileMode) (ed25519.PublicKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	path := filepath.Join(dir, "authority-key.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return pub, path
}

func envFromMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadSignerFromEnvAcceptsPKCS8PEMRegularOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	pub, path := writePKCS8KeyFile(t, dir, 0o600)

	signer, err := loadSignerFromEnv(envFromMap(map[string]string{
		"MULTICA_AUTHORITY_ID":               "local-dev-authority",
		"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": path,
	}), "darwin")
	if err != nil {
		t.Fatalf("LoadSignerFromEnv: %v", err)
	}
	if signer == nil {
		t.Fatal("signer is nil")
	}
	if signer.AuthorityID != "local-dev-authority" {
		t.Fatalf("AuthorityID = %q", signer.AuthorityID)
	}
	if got := EncodePublicKey(signer.PublicKey); got != EncodePublicKey(pub) {
		t.Fatalf("public key = %q, want %q", got, EncodePublicKey(pub))
	}
}

func TestLoadSignerFromEnvDisabledAndPartialConfig(t *testing.T) {
	signer, err := LoadSignerFromEnv(envFromMap(nil))
	if err != nil {
		t.Fatalf("disabled LoadSignerFromEnv: %v", err)
	}
	if signer != nil {
		t.Fatal("disabled config returned signer")
	}

	for name, env := range map[string]map[string]string{
		"missing_id": {
			"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": "/tmp/key.pem",
		},
		"missing_key_file": {
			"MULTICA_AUTHORITY_ID": "local-dev-authority",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSignerFromEnv(envFromMap(env)); err == nil {
				t.Fatal("partial config succeeded")
			}
		})
	}
}

func TestLoadSignerFromEnvRejectsUnsafeOrInvalidKeyFile(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writePKCS8KeyFile(t, dir, 0o600)

	t.Run("symlink", func(t *testing.T) {
		linkPath := filepath.Join(dir, "authority-link.pem")
		if err := os.Symlink(keyPath, linkPath); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		_, err := loadSignerFromEnv(envFromMap(map[string]string{
			"MULTICA_AUTHORITY_ID":               "local-dev-authority",
			"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": linkPath,
		}), "darwin")
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v, want symlink rejection", err)
		}
	})

	t.Run("loose permissions", func(t *testing.T) {
		_, loosePath := writePKCS8KeyFile(t, t.TempDir(), 0o644)
		_, err := loadSignerFromEnv(envFromMap(map[string]string{
			"MULTICA_AUTHORITY_ID":               "local-dev-authority",
			"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": loosePath,
		}), "darwin")
		if err == nil || !strings.Contains(err.Error(), "0600") {
			t.Fatalf("error = %v, want mode rejection", err)
		}
	})

	t.Run("invalid material", func(t *testing.T) {
		path := filepath.Join(dir, "invalid.pem")
		if err := os.WriteFile(path, []byte("not a private key"), 0o600); err != nil {
			t.Fatalf("write invalid: %v", err)
		}
		_, err := loadSignerFromEnv(envFromMap(map[string]string{
			"MULTICA_AUTHORITY_ID":               "local-dev-authority",
			"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": path,
		}), "darwin")
		if err == nil {
			t.Fatal("invalid key material succeeded")
		}
	})
}

func TestLoadPrivateKeyFileRejectsPathSwappedToSymlinkBeforeOpen(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("atomic no-follow key opening is Darwin-only")
	}

	dir := t.TempDir()
	_, keyPath := writePKCS8KeyFile(t, dir, 0o600)
	targetPath := filepath.Join(dir, "authority-key-target.pem")

	_, err := loadPrivateKeyFileWithOpener(keyPath, func(path string) (*os.File, error) {
		if err := os.Rename(path, targetPath); err != nil {
			t.Fatalf("rename checked key: %v", err)
		}
		if err := os.Symlink(targetPath, path); err != nil {
			t.Fatalf("replace checked key with symlink: %v", err)
		}
		return openPrivateKeyFileNoFollow(path)
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want atomic symlink rejection", err)
	}
}

func TestLoadSignerFromEnvAllowsConfiguredSignerInLinuxContainer(t *testing.T) {
	_, path := writePKCS8KeyFile(t, t.TempDir(), 0o600)
	signer, err := loadSignerFromEnv(envFromMap(map[string]string{
		"MULTICA_AUTHORITY_ID":               "local-dev-authority",
		"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": path,
	}), "linux")
	if err != nil {
		t.Fatalf("load signer for Linux container: %v", err)
	}
	if signer == nil || signer.AuthorityID != "local-dev-authority" {
		t.Fatalf("signer = %#v", signer)
	}
}

func TestLoadSignerFromEnvPublicEntryPointUsesRuntimePlatform(t *testing.T) {
	_, path := writePKCS8KeyFile(t, t.TempDir(), 0o600)
	signer, err := LoadSignerFromEnv(envFromMap(map[string]string{
		"MULTICA_AUTHORITY_ID":               "local-dev-authority",
		"MULTICA_AUTHORITY_PRIVATE_KEY_FILE": path,
	}))
	if err != nil {
		t.Fatalf("load signer on %s: %v", runtime.GOOS, err)
	}
	if signer == nil {
		t.Fatal("signer is nil")
	}
}

func TestValidatePrivateKeyFileMetadataRejectsWrongOwnerAndNonRegularFile(t *testing.T) {
	_, path := writePKCS8KeyFile(t, t.TempDir(), 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if err := validatePrivateKeyFileMetadata(info, os.Geteuid()+1); err == nil || !strings.Contains(err.Error(), "owned") {
		t.Fatalf("wrong-owner error = %v", err)
	}

	dirInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if err := validatePrivateKeyFileMetadata(dirInfo, os.Geteuid()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("non-regular error = %v", err)
	}
}
