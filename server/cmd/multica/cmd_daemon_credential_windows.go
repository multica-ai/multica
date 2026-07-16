//go:build windows

package main

import "os"

func openDaemonCredentialFile(path string) (*os.File, error) {
	return os.Open(path)
}
