//go:build verificationdev

package verificationenv

import (
	"os"
	"path/filepath"
)

// Home isolates development runtime registries and profile data by environment.
func Home(home string) string {
	root := os.Getenv("MULTICA_VERIFICATION_HOME")
	if os.Getenv("MULTICA_VERIFICATION_MODE") == "1" && len(os.Getenv("MULTICA_VERIFICATION_TOKEN")) == 64 && filepath.IsAbs(root) {
		return filepath.Clean(root)
	}
	return home
}
