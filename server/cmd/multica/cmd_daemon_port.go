package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type daemonPortRecord struct {
	Port int `json:"port"`
	PID  int `json:"pid"`
}

func daemonPortPathForProfile(profile string) string {
	return filepath.Join(daemonDirForProfile(profile), "daemon.port")
}

func readDaemonPortRecord(profile string) (daemonPortRecord, bool) {
	if daemonDirForProfile(profile) == "" {
		return daemonPortRecord{}, false
	}
	data, err := os.ReadFile(daemonPortPathForProfile(profile))
	if err != nil {
		return daemonPortRecord{}, false
	}
	var record daemonPortRecord
	if json.Unmarshal(data, &record) != nil || record.Port < 1 || record.Port > 65535 || record.PID < 1 {
		return daemonPortRecord{}, false
	}
	return record, true
}

// resolveHealthPort treats daemon.port as a hint, never as proof of ownership.
// A crash or systemd handoff can leave it behind. Old daemons without this file
// remain discoverable at the profile-derived port.
func resolveHealthPort(profile string) int {
	if record, ok := readDaemonPortRecord(profile); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		health := checkDaemonHealthOnPort(ctx, record.Port)
		cancel()
		if daemonAlive(health) && health["profile"] == profile && health["pid"] == float64(record.PID) {
			return record.Port
		}
	}
	return healthPortForProfile(profile)
}

func publishDaemonPort(profile string, record daemonPortRecord) error {
	dir := daemonDirForProfile(profile)
	if dir == "" {
		return fmt.Errorf("resolve daemon profile directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".daemon.port-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	data, err := json.Marshal(record)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	path := daemonPortPathForProfile(profile)
	for attempt := 0; ; attempt++ {
		err = os.Rename(tmp, path)
		if err == nil || runtime.GOOS != "windows" || attempt == 9 {
			return err
		}
		// Windows readers can momentarily deny replacement of the old file.
		time.Sleep(50 * time.Millisecond)
	}
}

// The holder still owns the profile lock when this runs, so a successor cannot
// publish its record between the ownership check and the remove.
func removeDaemonPortIfOwned(profile string, pid int) {
	if record, ok := readDaemonPortRecord(profile); ok && record.PID == pid {
		_ = os.Remove(daemonPortPathForProfile(profile))
	}
}

// bindDaemonHealthPort holds the successful listener. A probe-and-close would
// leave a race for another process to claim the selected port.
func bindDaemonHealthPort(preferred int, listen func(string, string) (net.Listener, error)) (net.Listener, int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", preferred)
	ln, preferredErr := listen("tcp", addr)
	if preferredErr == nil {
		return ln, preferred, nil
	}
	ln, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		hint := ""
		if runtime.GOOS == "windows" {
			hint = "; inspect reserved ports with 'netsh interface ipv4 show excludedportrange protocol=tcp'"
		}
		return nil, 0, fmt.Errorf("bind health port %s: %w; fallback bind: %v%s", addr, preferredErr, err, hint)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(os.Stderr, "Health port %s could not be bound: %v; using 127.0.0.1:%d instead.\n", addr, preferredErr, port)
	return ln, port, nil
}
