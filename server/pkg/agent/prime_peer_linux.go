//go:build linux

package agent

import (
	"errors"
	"fmt"
	"net"
	"os"
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

func primeProcessStartToken(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	line := string(raw)
	commandEnd := strings.LastIndex(line, ")")
	if commandEnd < 0 || commandEnd+2 >= len(line) {
		return "", errors.New("invalid proc stat")
	}
	fields := strings.Fields(line[commandEnd+2:])
	if len(fields) <= 19 || fields[19] == "" {
		return "", errors.New("proc start token missing")
	}
	return "proc:" + fields[19], nil
}
