//go:build windows

package desktopdictation

import (
	"testing"
	"unsafe"
)

// These tests only encode memory and inspect fake identities; never enumerate
// user processes or call SendInput during the default test suite.
func TestWindowsInputLayout(t *testing.T) {
	wantSize, wantOffset := uintptr(28), uintptr(4)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantSize, wantOffset = 40, 8
	}
	if unsafe.Sizeof(nativeInput{}) != wantSize || unsafe.Offsetof(nativeInput{}.Data) != wantOffset {
		t.Fatal("INPUT size/alignment does not match the Windows ABI")
	}
	inputs := encodeKeys([]keyEvent{{key: 0x11}, {key: 0x11, up: true}})
	for i := range inputs {
		keyboard := (*keyboardInput)(unsafe.Pointer(&inputs[i].Data))
		if inputs[i].Kind != 1 || keyboard.VirtualKey != 0x11 || keyboard.Flags != uint32(i*2) ||
			keyboard.Scan != 0 || keyboard.Time != 0 || keyboard.ExtraInfo != 0 {
			t.Fatalf("unexpected keyboard encoding %#v / %#v", inputs[i], keyboard)
		}
	}
}

func TestOnlyVerifiedPackagedDesktopIsEligible(t *testing.T) {
	for _, tc := range []struct {
		name, path, family string
		want               bool
	}{
		{"ChatGPT.exe", `C:\Program Files\WindowsApps\OpenAI.Codex_version\app\ChatGPT.exe`, codexPackageFamily, true},
		{"Codex.exe", `C:\Program Files\WindowsApps\OpenAI.Codex_version\app\Codex.exe`, codexPackageFamily, true},
		{"ChatGPT.exe", `C:\fake\app\ChatGPT.exe`, "", false},
		{"ChatGPT.exe", `C:\fake\app\ChatGPT.exe`, "OpenAI.Codex_otherpublisher", false},
		{"codex.exe", `C:\Program Files\WindowsApps\OpenAI.Codex_version\app\resources\codex.exe`, codexPackageFamily, false},
		{"Other.exe", `C:\Program Files\WindowsApps\OpenAI.Codex_version\app\Other.exe`, codexPackageFamily, false},
	} {
		if got := isCodexDesktop(tc.name, tc.path, tc.family); got != tc.want {
			t.Errorf("identity %q / %q: got %v, want %v", tc.name, tc.family, got, tc.want)
		}
	}
}
