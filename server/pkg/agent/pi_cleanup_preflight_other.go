//go:build !darwin

package agent

func checkPiCleanupSupport() error { return nil }
