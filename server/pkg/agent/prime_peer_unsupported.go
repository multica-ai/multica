//go:build !linux && !darwin

package agent

import (
	"errors"
	"net"
)

func primePeerCredentialsSupported() bool { return false }

func kernelPrimePeerIdentity(net.Conn) (int, int, error) {
	return 0, 0, errors.New("kernel-authenticated Unix peer PID credentials are unavailable")
}

func primeProcessStartToken(int) (string, error) {
	return "", errors.New("process start identity is unavailable")
}
