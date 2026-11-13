package wecom

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func createMediaTempFile(dir string) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	// Protect the DACL from inherited directory permissions. Chmod(0600)
	// on Windows only sets the read-only attribute, not access permissions.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	attrs := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}
	if dir == "" {
		dir = os.TempDir()
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// Match os.CreateTemp's support for paths beyond MAX_PATH, including UNC.
	if !strings.HasPrefix(dir, `\\?\`) {
		if strings.HasPrefix(dir, `\\`) {
			dir = `\\?\UNC\` + strings.TrimPrefix(dir, `\\`)
		} else {
			dir = `\\?\` + dir
		}
	}
	for range 10 {
		name := filepath.Join(dir, "wecom-media-"+rand.Text()+".bin")
		path, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		// No sharing or inheritance: another handle cannot expose the
		// plaintext or postpone deletion after this process exits.
		h, err := windows.CreateFile(path,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
			0, &attrs, windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_TEMPORARY|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, &os.PathError{Op: "createtemp", Path: name, Err: err}
		}
		return os.NewFile(uintptr(h), name), nil
	}
	return nil, &os.PathError{Op: "createtemp", Path: dir, Err: os.ErrExist}
}
