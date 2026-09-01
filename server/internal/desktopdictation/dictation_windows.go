//go:build windows

package desktopdictation

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const codexPackageFamily = "OpenAI.Codex_2p2nqsd0c76g0"

var (
	dictationUser32   = windows.NewLazySystemDLL("user32.dll")
	sendInputProc     = dictationUser32.NewProc("SendInput")
	keyStateProc      = dictationUser32.NewProc("GetAsyncKeyState")
	packageFamilyProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPackageFamilyName")
)

// INPUT's union is sized/aligned for MOUSEINPUT, even when sending KEYBDINPUT.
// A keyboard-only struct would pass the wrong cbSize (40 on x64, 28 on x86).
type mouseInput struct {
	X, Y      int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keyboardInput struct {
	VirtualKey uint16
	Scan       uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

type nativeInput struct {
	Kind uint32
	Data mouseInput
}

type windowsPlatform struct{}

func nativePlatform() platform { return windowsPlatform{} }

func (windowsPlatform) available() bool {
	return sendInputProc.Find() == nil && keyStateProc.Find() == nil && packageFamilyProc.Find() == nil
}

func (windowsPlatform) foregroundWindow() uintptr {
	return uintptr(windows.GetForegroundWindow())
}

func (windowsPlatform) keyDown(key uint16) bool {
	state, _, _ := keyStateProc.Call(uintptr(key))
	return state&0x8000 != 0
}

func isCodexDesktop(name, imagePath, family string) bool {
	return family == codexPackageFamily &&
		(strings.EqualFold(name, "ChatGPT.exe") || strings.EqualFold(name, "Codex.exe")) &&
		strings.EqualFold(filepath.Base(imagePath), name) &&
		strings.EqualFold(filepath.Base(filepath.Dir(imagePath)), "app")
}

func processIsCodexDesktop(pid uint32, name string, session uint32) bool {
	var candidateSession uint32
	if windows.ProcessIdToSessionId(pid, &candidateSession) != nil || candidateSession != session {
		return false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	// Ask Windows for package identity; a lookalike executable name/path alone
	// is not proof that the official app owns the global shortcut.
	var family [128]uint16
	familyLength := uint32(len(family))
	status, _, _ := packageFamilyProc.Call(uintptr(process), uintptr(unsafe.Pointer(&familyLength)), uintptr(unsafe.Pointer(&family[0])))
	if status != 0 {
		return false
	}
	var image [32768]uint16
	imageLength := uint32(len(image))
	if windows.QueryFullProcessImageName(process, 0, &image[0], &imageLength) != nil {
		return false
	}
	return isCodexDesktop(name, windows.UTF16ToString(image[:]), windows.UTF16ToString(family[:]))
}

func (windowsPlatform) desktopRunning() bool {
	var session uint32
	if windows.ProcessIdToSessionId(uint32(os.Getpid()), &session) != nil {
		return false
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if (strings.EqualFold(name, "ChatGPT.exe") || strings.EqualFold(name, "Codex.exe")) &&
			processIsCodexDesktop(entry.ProcessID, name, session) {
			return true
		}
	}
	return false
}

func encodeKeys(keys []keyEvent) []nativeInput {
	inputs := make([]nativeInput, len(keys))
	for i, key := range keys {
		inputs[i].Kind = 1 // INPUT_KEYBOARD
		keyboard := (*keyboardInput)(unsafe.Pointer(&inputs[i].Data))
		keyboard.VirtualKey = key.key
		if key.up {
			keyboard.Flags = 2 // KEYEVENTF_KEYUP
		}
	}
	return inputs
}

func (windowsPlatform) sendKeys(keys []keyEvent) int {
	if len(keys) == 0 {
		return 0
	}
	inputs := encodeKeys(keys)
	// Windows enforces UIPI; a blocked call fails closed. No elevation or
	// accessibility/security setting changes are attempted by this helper.
	sent, _, _ := sendInputProc.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(nativeInput{}))
	runtime.KeepAlive(inputs)
	return int(sent)
}
