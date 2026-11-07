//go:build !linux

package gitca

import "io/fs"

// crypto/x509 reads SSL_CERT_DIR only on Unix-like systems, with per-OS
// directory lists. The backend ships as a Linux image, so other platforms
// share nothing rather than guess at a list the standard library may not use.
const certDirEnv = ""

var systemCertDirectories []string

func readUniqueDirectoryEntries(string) ([]fs.DirEntry, error) { return nil, nil }
