package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtListedInPathext(t *testing.T) {
	t.Parallel()

	pathext := ".COM;.EXE;.BAT;.CMD"
	cases := []struct {
		path string
		list string
		want bool
	}{
		{path: `C:\tools\openclaw.exe`, list: pathext, want: true},
		{path: `C:\tools\openclaw.EXE`, list: pathext, want: true},
		{path: `C:\tools\openclaw.cmd`, list: pathext, want: true},
		{path: `C:\tools\openclaw.CMD`, list: pathext, want: true},
		{path: `C:\tools\openclaw.bat`, list: pathext, want: true},
		{path: `C:\tools\notes.txt`, list: pathext, want: false},
		{path: `C:\tools\openclaw`, list: pathext, want: false},
		{path: `C:\tools\openclaw.exe`, list: "", want: true}, // default list
		{path: `C:\tools\notes.txt`, list: "", want: false},
		{path: `C:\tools\openclaw.exe`, list: ".CMD", want: false},
		{path: `/usr/bin/openclaw.cmd`, list: "CMD;EXE", want: true}, // missing dots
	}
	for _, tc := range cases {
		if got := extListedInPathext(tc.path, tc.list); got != tc.want {
			t.Errorf("extListedInPathext(%q, %q) = %v, want %v", tc.path, tc.list, got, tc.want)
		}
	}
}

func TestFileLooksLaunchable_UnixBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not the Windows gate")
	}

	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileLooksLaunchable(tool) {
		t.Fatalf("0644 file should not look launchable on unix")
	}
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	if !fileLooksLaunchable(tool) {
		t.Fatalf("0755 file should look launchable on unix")
	}
	if fileLooksLaunchable(dir) {
		t.Fatalf("directory should not look launchable")
	}
	if fileLooksLaunchable(filepath.Join(dir, "missing")) {
		t.Fatalf("missing path should not look launchable")
	}
}

func TestFileLooksLaunchable_WindowsPathext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows file modes never set 0111; this is the host gate")
	}

	dir := t.TempDir()
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	exe := filepath.Join(dir, "tool.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileLooksLaunchable(exe) {
		t.Fatalf("plain .exe should look launchable on Windows, even without unix exec bits")
	}

	cmd := filepath.Join(dir, "tool.cmd")
	if err := os.WriteFile(cmd, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileLooksLaunchable(cmd) {
		t.Fatalf(".cmd shim should look launchable on Windows")
	}

	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileLooksLaunchable(txt) {
		t.Fatalf(".txt should not look launchable on Windows")
	}

	if fileLooksLaunchable(dir) {
		t.Fatalf("directory should not look launchable")
	}
}
