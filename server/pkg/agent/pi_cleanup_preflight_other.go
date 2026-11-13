//go:build !darwin && !linux

package agent

func checkPiCleanupSupport() error { return nil }
