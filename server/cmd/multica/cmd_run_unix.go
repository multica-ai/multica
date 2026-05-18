//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"os/signal"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func watchTerminalResize(ptmx *os.File) func() {
	_ = pty.InheritSize(os.Stdin, ptmx)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				_ = pty.InheritSize(os.Stdin, ptmx)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

func makeStdinRaw() (func(), error) {
	fd := int(os.Stdin.Fd())
	if !isTerminal(fd) {
		return func() {}, nil
	}
	oldState, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, fmt.Errorf("read terminal state: %w", err)
	}
	raw := *oldState
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return nil, fmt.Errorf("set terminal raw mode: %w", err)
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, oldState)
	}, nil
}

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	return err == nil
}
