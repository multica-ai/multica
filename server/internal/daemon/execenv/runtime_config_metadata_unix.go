//go:build linux

package execenv

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// preserveRuntimeConfigFileMetadata clones the inode metadata that an atomic
// rename would otherwise discard. Ownership is applied before mode bits
// because chown can clear set-ID bits; extended attributes are copied exactly,
// including POSIX ACLs where the filesystem exposes them as xattrs.
func preserveRuntimeConfigFileMetadata(staged *os.File, sourcePath string, mode fs.FileMode) error {
	if sourcePath == "" {
		return staged.Chmod(mode)
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open metadata source: %w", err)
	}
	defer source.Close()

	var sourceStat unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &sourceStat); err != nil {
		return fmt.Errorf("stat metadata source: %w", err)
	}
	var stagedStat unix.Stat_t
	if err := unix.Fstat(int(staged.Fd()), &stagedStat); err != nil {
		return fmt.Errorf("stat staged file: %w", err)
	}
	if stagedStat.Uid != sourceStat.Uid || stagedStat.Gid != sourceStat.Gid {
		if err := unix.Fchown(int(staged.Fd()), int(sourceStat.Uid), int(sourceStat.Gid)); err != nil {
			return fmt.Errorf("copy owner and group: %w", err)
		}
	}
	if err := unix.Fchmod(int(staged.Fd()), uint32(sourceStat.Mode)&0o7777); err != nil {
		return fmt.Errorf("copy file mode: %w", err)
	}

	sourceAttrs, err := readRuntimeConfigXattrs(int(source.Fd()))
	if err != nil {
		return fmt.Errorf("read source extended attributes: %w", err)
	}
	stagedAttrs, err := listRuntimeConfigXattrs(int(staged.Fd()))
	if err != nil {
		return fmt.Errorf("list staged extended attributes: %w", err)
	}
	for _, name := range stagedAttrs {
		if _, keep := sourceAttrs[name]; keep {
			continue
		}
		if err := unix.Fremovexattr(int(staged.Fd()), name); err != nil {
			return fmt.Errorf("remove inherited extended attribute %q: %w", name, err)
		}
	}
	for name, value := range sourceAttrs {
		if err := unix.Fsetxattr(int(staged.Fd()), name, value, 0); err != nil {
			return fmt.Errorf("copy extended attribute %q: %w", name, err)
		}
	}

	if err := unix.Fstat(int(staged.Fd()), &stagedStat); err != nil {
		return fmt.Errorf("verify staged metadata: %w", err)
	}
	if stagedStat.Uid != sourceStat.Uid || stagedStat.Gid != sourceStat.Gid {
		return fmt.Errorf("staged owner/group = %d:%d, want %d:%d", stagedStat.Uid, stagedStat.Gid, sourceStat.Uid, sourceStat.Gid)
	}
	if got, want := uint32(stagedStat.Mode)&0o7777, uint32(sourceStat.Mode)&0o7777; got != want {
		return fmt.Errorf("staged mode = %04o, want %04o", got, want)
	}
	return nil
}

func readRuntimeConfigXattrs(fd int) (map[string][]byte, error) {
	names, err := listRuntimeConfigXattrs(fd)
	if err != nil {
		return nil, err
	}
	attrs := make(map[string][]byte, len(names))
	for _, name := range names {
		value, err := readRuntimeConfigXattr(fd, name)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", name, err)
		}
		attrs[name] = value
	}
	return attrs, nil
}

func listRuntimeConfigXattrs(fd int) ([]string, error) {
	for {
		size, err := unix.Flistxattr(fd, nil)
		if err != nil {
			return nil, err
		}
		if size == 0 {
			return nil, nil
		}
		buf := make([]byte, size)
		n, err := unix.Flistxattr(fd, buf)
		if err == unix.ERANGE {
			continue
		}
		if err != nil {
			return nil, err
		}
		parts := bytes.Split(bytes.TrimRight(buf[:n], "\x00"), []byte{0})
		names := make([]string, 0, len(parts))
		for _, part := range parts {
			if len(part) != 0 {
				names = append(names, string(part))
			}
		}
		return names, nil
	}
}

func readRuntimeConfigXattr(fd int, name string) ([]byte, error) {
	for {
		size, err := unix.Fgetxattr(fd, name, nil)
		if err != nil {
			return nil, err
		}
		value := make([]byte, size)
		n, err := unix.Fgetxattr(fd, name, value)
		if err == unix.ERANGE {
			continue
		}
		if err != nil {
			return nil, err
		}
		return value[:n], nil
	}
}
