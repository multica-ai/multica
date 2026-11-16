//go:build !linux && !darwin

package agent

import "context"

// Windows retains its Job Object and ordinary Wait/release lifecycle.
func (p *ownedRuntimeProcess) startExitObserver(context.Context, <-chan struct{}, func()) {}
