//go:build !darwin

package daemon

func filesystemFreeGiB(string) (uint64, error) {
	return 0, nil
}
