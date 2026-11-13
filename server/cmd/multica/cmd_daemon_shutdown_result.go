package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type daemonShutdownResult struct {
	PID              int  `json:"pid"`
	CleanupConfirmed bool `json:"cleanup_confirmed"`
}

func daemonShutdownResultPath(profile string) string {
	return filepath.Join(daemonDirForProfile(profile), "daemon.shutdown.json")
}

// Written only while the daemon holds the profile lock, first as unconfirmed
// at startup, then with the final cleanup outcome before releasing the lock.
func writeDaemonShutdownResult(profile string, result daemonShutdownResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(daemonShutdownResultPath(profile), data, 0o600)
}

func confirmDaemonCleanup(profile string, pid int) error {
	result, err := readDaemonShutdownResult(profile)
	if err != nil || pid <= 1 || result.PID != pid || !result.CleanupConfirmed {
		// A daemon without a matching result (for example one started by an
		// older CLI) leaves nothing behind after it exits. Persist the failure so
		// a repeated stop cannot treat the now-idle profile as cleanly stopped.
		// The next daemon start overwrites this record with its own PID.
		if err != nil || result.PID != pid || result.CleanupConfirmed {
			_ = writeDaemonShutdownResult(profile, daemonShutdownResult{PID: max(pid, 0)})
		}
		return fmt.Errorf("daemon exited but task cleanup could not be confirmed; background tasks may still be running")
	}
	return nil
}

func readDaemonShutdownResult(profile string) (daemonShutdownResult, error) {
	var result daemonShutdownResult
	data, err := os.ReadFile(daemonShutdownResultPath(profile))
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}

func daemonStopPID(profile string, health map[string]any) int {
	if value, ok := health["pid"].(float64); ok && value > 1 && float64(int(value)) == value {
		return int(value)
	}
	if data, err := os.ReadFile(daemonPIDPathForProfile(profile)); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
			return pid
		}
	}
	if record, ok := readDaemonPortRecord(profile); ok {
		return record.PID
	}
	return 0
}
