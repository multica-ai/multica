package gitca

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// certDirEnv matches crypto/x509's certDirEnv.
const certDirEnv = "SSL_CERT_DIR"

// systemCertDirectories matches crypto/x509's certDirectories on Linux. These
// hold the public roots every daemon already has, so they are not shared.
var systemCertDirectories = []string{
	"/etc/ssl/certs",     // SLES10/SLES11
	"/etc/pki/tls/certs", // Fedora/RHEL
}

// readUniqueDirectoryEntries matches crypto/x509's helper of the same name:
// os.ReadDir without symlinks that point within the directory, which
// c_rehash creates as hash aliases of the real files.
func readUniqueDirectoryEntries(dir string) ([]fs.DirEntry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	uniq := files[:0]
	for _, f := range files {
		if !isSameDirSymlink(f, dir) {
			uniq = append(uniq, f)
		}
	}
	return uniq, nil
}

func isSameDirSymlink(f fs.DirEntry, dir string) bool {
	if f.Type()&fs.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(filepath.Join(dir, f.Name()))
	return err == nil && !strings.Contains(target, "/")
}
