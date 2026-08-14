//go:build darwin

package agent

import (
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"

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
	var pid int
	var cred *unix.Xucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		pid, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if sockErr == nil {
			cred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		}
	}); err != nil {
		return 0, 0, err
	}
	if sockErr != nil || cred == nil {
		return 0, 0, sockErr
	}
	return pid, int(cred.Uid), nil
}

func primeProcessStartToken(pid int) (string, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return "", err
	}
	start := strings.TrimSpace(string(out))
	if start == "" {
		return "", errors.New("process start token missing")
	}
	return "ps:" + start, nil
}
