//go:build !windows

package agent

func hermesGatewayReserved(string) (bool, error) { return false, nil }
