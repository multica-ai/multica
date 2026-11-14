package execenv

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// Managed paths are rooted at the daemon-owned environment directory. This is
// defense in depth against accidental/previous-run symlinks, not a sandbox
// against another process with the daemon's UID. Never chmod the user's include.
func openOpenclawManagedRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid OpenClaw managed root")
	}
	return os.OpenRoot(path)
}

func writeOpenclawPrivateFile(root, name string, data []byte) error {
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return fmt.Errorf("invalid OpenClaw managed filename")
	}
	dir, err := openOpenclawManagedRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	if info, err := dir.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("OpenClaw managed file must be regular")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Exclusive creation and rename prevent following a preexisting file/symlink,
	// enforce 0600 on reuse, and leave the old complete file intact on write error.
	temp := ".openclaw-" + rand.Text()
	file, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return dir.Rename(temp, name)
}

func ensureOpenclawPrivateDirectory(root, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("invalid OpenClaw directory")
	}
	dir, err := openOpenclawManagedRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	// Walk all managed components, never chmod through a path. Root confines
	// concurrent resolution; the open file handle pins the directory for chmod.
	path := ""
	for _, part := range splitOpenclawPath(relative) {
		path = filepath.Join(path, part)
		if err := dir.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := dir.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("OpenClaw private directory contains symlink or non-directory")
		}
		file, err := dir.Open(path)
		if err != nil {
			return err
		}
		err = file.Chmod(0700)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func splitOpenclawPath(path string) []string {
	var parts []string
	for path != "." && path != "" {
		parts = append([]string{filepath.Base(path)}, parts...)
		path = filepath.Dir(path)
	}
	return parts
}
