//go:build !windows

package execenv

import (
	"fmt"
	"io/fs"
	"reflect"
)

// validateRuntimeConfigReplacementTarget rejects multiple-hard-link files.
// Replacing only one directory entry would otherwise detach it from the
// user-owned inode, so cleanup could not restore the original identity.
// Stat_t.Nlink varies in integer width across Unix targets, hence the small
// reflective adapter here.
func validateRuntimeConfigReplacementTarget(path string, info fs.FileInfo) error {
	stat := reflect.ValueOf(info.Sys())
	if stat.Kind() == reflect.Pointer {
		if stat.IsNil() {
			return fmt.Errorf("inspect runtime config %s link count: missing stat data", path)
		}
		stat = stat.Elem()
	}
	nlink := stat.FieldByName("Nlink")
	if !nlink.IsValid() {
		return fmt.Errorf("inspect runtime config %s link count: unsupported stat data", path)
	}
	var count uint64
	switch nlink.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		count = nlink.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if nlink.Int() < 0 {
			return fmt.Errorf("inspect runtime config %s link count: invalid value %d", path, nlink.Int())
		}
		count = uint64(nlink.Int())
	default:
		return fmt.Errorf("inspect runtime config %s link count: unsupported field type %s", path, nlink.Kind())
	}
	if count != 1 {
		return fmt.Errorf("runtime config %s has %d hard links; refusing atomic replacement", path, count)
	}
	return nil
}
