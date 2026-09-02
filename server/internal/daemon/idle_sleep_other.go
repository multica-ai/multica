//go:build !darwin

package daemon

import "log/slog"

type noopIdleSleepAssertion struct{}

func newIdleSleepAssertion(*slog.Logger) idleSleepAssertion {
	return noopIdleSleepAssertion{}
}

func (noopIdleSleepAssertion) Acquire() error { return nil }
func (noopIdleSleepAssertion) Release()       {}
