//go:build !windows

package execenv

func isRetryableTaskRootRenameError(error) bool {
	return false
}
