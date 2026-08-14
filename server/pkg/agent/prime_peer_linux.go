//go:build linux

package agent

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func primePeerCredentialsSupported() bool { return true }

func kernelPrimePeerIdentity(conn net.Conn) (int, int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, errors.New("Prime daemon connection is not Unix")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if sockErr != nil || cred == nil {
		return 0, 0, sockErr
	}
	return int(cred.Pid), int(cred.Uid), nil
}
