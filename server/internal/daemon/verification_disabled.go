//go:build !verificationdev

package daemon

import "context"

func verificationAgents() (map[string]AgentEntry, bool)         { return nil, false }
func (d *Daemon) runVerification(context.Context) (bool, error) { return false, nil }
