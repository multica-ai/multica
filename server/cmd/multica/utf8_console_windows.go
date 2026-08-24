//go:build windows

package main

import "golang.org/x/sys/windows"

const utf8CodePage = 65001

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

func configureUTF8Console() {
	_, _, _ = procSetConsoleCP.Call(utf8CodePage)
	_, _, _ = procSetConsoleOutputCP.Call(utf8CodePage)
}
