//go:build windows

package kagent

import (
	"fmt"
	"os"
)

func lockFile(*os.File) error {
	return fmt.Errorf("kagent: local DEV journal requires Unix filesystem locks")
}
