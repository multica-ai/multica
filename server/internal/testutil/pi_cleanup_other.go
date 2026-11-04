//go:build !linux && !darwin

package testutil

func RunPiCleanupFixture() bool { return false }
