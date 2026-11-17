//go:build !windows

package wecom

import (
	"errors"
	"os"
)

func createMediaTempFile(dir string) (*os.File, error) {
	out, err := os.CreateTemp(dir, "wecom-media-*.bin") // Created with mode 0600.
	if err != nil {
		return nil, err
	}
	// Unlink before writing plaintext, so even an abnormal process exit
	// cannot leave attachment content behind.
	if err := os.Remove(out.Name()); err != nil {
		return nil, errors.Join(err, out.Close(), os.Remove(out.Name()))
	}
	return out, nil
}
