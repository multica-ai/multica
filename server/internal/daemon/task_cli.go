package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

func configureTaskCLIEnvironment(env map[string]string) error {
	selfBin, err := resolveSelfExecutable()
	if err != nil {
		return fmt.Errorf("resolve task Multica CLI: %w", err)
	}
	// Login shell startup files can reorder PATH after the provider starts.
	// Keep an explicit executable for the commands in the runtime brief.
	env["MULTICA_CLI_PATH"] = selfBin
	env["PATH"] = filepath.Dir(selfBin) + string(os.PathListSeparator) + os.Getenv("PATH")
	return nil
}
