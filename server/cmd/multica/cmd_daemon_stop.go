package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The daemon holds this lock through task cleanup and runtime deregistration,
// after its health listener has closed. Release confirms daemon termination,
// but only the shutdown result can confirm that task processes were cleaned up.
func daemonStopComplete(profile string) (bool, error) {
	path := filepath.Join(daemonDirForProfile(profile), "daemon.lock")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true, nil
	}
	lock, acquired, err := tryLockDaemonProfile(path)
	if lock != nil {
		if closeErr := lock.Close(); err == nil {
			err = closeErr
		}
	}
	return acquired, err
}

func waitDaemonStop(profile string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		stopped, err := daemonStopComplete(profile)
		if stopped || err != nil || !time.Now().Before(deadline) {
			return stopped, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func stopDaemonAndWait(profile string, port int, health map[string]any, grace, killWait time.Duration) error {
	if daemonAlive(health) {
		if err := daemonIdentityMismatch(health, profile, port); err != nil {
			return err
		}
		if hostOS, _ := health["os"].(string); hostOS != "" && hostOS != runtime.GOOS {
			return fmt.Errorf("daemon runs on %s; the native CLI cannot stop it", hostOS)
		}
	}
	stopped, err := daemonStopComplete(profile)
	if err != nil {
		return err
	}
	if stopped && !daemonAlive(health) {
		// No daemon was observed in this call. Preserve the idle no-op, but do
		// not turn a previous failed cleanup into success on a repeated quit.
		result, err := readDaemonShutdownResult(profile)
		if os.IsNotExist(err) {
			return nil
		}
		return confirmDaemonCleanup(profile, result.PID)
	}
	pidAtStop := daemonStopPID(profile, health)
	// Keep the official cancellation path, including Agent process-group cleanup.
	_ = requestDaemonShutdown(port)
	if stopped {
		return fmt.Errorf("shutdown requested but the daemon has no profile lock; cannot confirm completion")
	}
	// The endpoint may already be closed while a previous shutdown is cleaning
	// up. A failed request must not shorten the lock holder's grace period.
	if stopped, err = waitDaemonStop(profile, grace); err != nil || stopped {
		if err != nil {
			return err
		}
		return confirmDaemonCleanup(profile, pidAtStop)
	}
	// A fresh health identity is preferred. If health has closed or wedged,
	// validate the profile PID file against the OS command before signalling it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health = checkDaemonHealthOnPort(ctx, port)
	var pid int
	if daemonAlive(health) {
		if err := daemonIdentityMismatch(health, profile, port); err != nil {
			return err
		}
		if hostOS, _ := health["os"].(string); hostOS != "" && hostOS != runtime.GOOS {
			return fmt.Errorf("refusing to terminate a daemon on %s", hostOS)
		}
		value, _ := health["pid"].(float64)
		pid = int(value)
		if float64(pid) != value {
			return fmt.Errorf("invalid daemon PID")
		}
	} else {
		data, err := os.ReadFile(daemonPIDPathForProfile(profile))
		if os.IsNotExist(err) {
			// An earlier plain stop can remove daemon.pid when health closes,
			// while the daemon still owns its port record and shutdown lock.
			if record, ok := readDaemonPortRecord(profile); ok {
				data, err = []byte(strconv.Itoa(record.PID)), nil
			}
		}
		if err != nil {
			if stopped, probeErr := daemonStopComplete(profile); probeErr == nil && stopped {
				return confirmDaemonCleanup(profile, pidAtStop)
			}
			return fmt.Errorf("read daemon PID before forced stop: %w", err)
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 1 || pid == os.Getpid() {
			return fmt.Errorf("invalid daemon PID file")
		}
		if err := verifyDaemonStopProcess(pid, profile); err != nil {
			if stopped, probeErr := daemonStopComplete(profile); probeErr == nil && stopped {
				return confirmDaemonCleanup(profile, pidAtStop)
			}
			return err
		}
	}
	if pid <= 1 || pid == os.Getpid() {
		return fmt.Errorf("invalid daemon PID")
	}
	if stopped, err = daemonStopComplete(profile); err != nil || stopped {
		if err != nil {
			return err
		}
		return confirmDaemonCleanup(profile, pidAtStop)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer process.Release()
	if err := process.Kill(); err != nil {
		if stopped, probeErr := daemonStopComplete(profile); probeErr == nil && stopped {
			return confirmDaemonCleanup(profile, pidAtStop)
		}
		return fmt.Errorf("force stop daemon: %w", err)
	}
	if stopped, err = waitDaemonStop(profile, killWait); err != nil {
		return err
	}
	if stopped {
		// Killing the daemon bypasses its cancellation handlers. In particular,
		// Unix Agent processes own independent process groups and can survive.
		// Even an accepted shutdown request does not prove cleanup completed.
		return fmt.Errorf("daemon was forcibly terminated; task cleanup could not be confirmed and background tasks may still be running")
	}
	return fmt.Errorf("daemon profile %q is still locked after forced termination", profile)
}

func verifyDaemonStopProcess(pid int, profile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var name, command string
	if runtime.GOOS == "windows" {
		script := fmt.Sprintf("Get-CimInstance Win32_Process -Filter 'ProcessId = %d' | Select-Object Name,CommandLine | ConvertTo-Json -Compress", pid)
		data, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
		if err != nil {
			return err
		}
		var record struct{ Name, CommandLine string }
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		name, command = record.Name, record.CommandLine
	} else {
		data, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		if err != nil {
			return err
		}
		name = strings.TrimSpace(string(data))
		data, err = exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
		if err != nil {
			return err
		}
		command = string(data)
	}
	profileMatches := !strings.Contains(command, "--profile") && profile == ""
	if profile != "" {
		profileMatches = regexp.MustCompile(`--profile(?:=|\s+)"?` + regexp.QuoteMeta(profile) + `"?(?:\s|$)`).MatchString(command)
	}
	base := filepath.Base(name)
	if (base != "multica" && base != "multica.exe") || !regexp.MustCompile(`\bdaemon\s+start\b`).MatchString(command) || !profileMatches {
		return fmt.Errorf("PID %d does not identify the daemon for profile %q", pid, profile)
	}
	return nil
}
