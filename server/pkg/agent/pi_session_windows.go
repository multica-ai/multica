//go:build windows

package agent

import (
	"fmt"
	"os"
)

type piSessionFile struct {
	file *os.File
}

func (s *piSessionFile) Close() error      { return nil }
func (s *piSessionFile) childPath() string { return "" }

func createPiSessionFile(taskTempDir, sessionID string) (*piSessionFile, error) {
	return nil, fmt.Errorf("Pi execution is disabled on Windows until secure session handle inheritance is available")
}
