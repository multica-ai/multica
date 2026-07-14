//go:build !darwin && !linux

package authority

import (
	"errors"
	"os"
)

func openPrivateKeyFileNoFollow(string) (*os.File, error) {
	return nil, errors.New("atomic authority private key opening is unsupported on this platform")
}
