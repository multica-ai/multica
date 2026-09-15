package daemon

import (
	"context"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// Resolve policy only after Prepare/Reuse has established the final cwd and
// runtime environment. User-level inventory alone can re-enable project-disabled plugins.
func (d *Daemon) prepareTaskClaudePlugins(ctx context.Context, cfg agent.Config, opts agent.ExecOptions, env *execenv.Environment, disabled []execenv.RuntimeSkillRefForEnv) error {
	requested := false
	for _, ref := range disabled {
		requested = requested || ref.Root == "plugin"
	}
	if !requested {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, d.effectiveTaskPrepareTimeout())
	defer cancel()
	plugins, err := agent.QueryClaudePlugins(ctx, cfg, opts)
	if err != nil {
		return err
	}
	configDir, err := agent.ClaudeConfigDirectory(cfg, opts.Cwd)
	if err != nil {
		return err
	}
	params := execenv.ClaudePluginCopiesParams{
		RootDir: env.RootDir, WorkDir: opts.Cwd, ConfigDir: configDir,
		Disabled: disabled, Plugins: plugins,
	}
	var dirs []string
	if d.executionEnvironmentCommand == nil {
		dirs, err = execenv.PrepareClaudePluginCopies(params)
	} else {
		command, commandErr := d.executionEnvironmentCommand()
		if commandErr != nil {
			return commandErr
		}
		dirs, err = execenv.PrepareClaudePluginCopiesIsolated(ctx, command, params, d.logger)
	}
	if err != nil {
		return err
	}
	env.ClaudePluginDirs = dirs
	return nil
}
