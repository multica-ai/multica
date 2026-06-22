# Custom runtimes

Custom runtime profiles let a workspace register an AI CLI that speaks one of
Multica's supported protocol families but is launched through a team-specific
command.

## Command and arguments

Paste the same argv-style command you would run in a terminal:

```sh
agent --model composer-2.5
```

Multica stores this as:

- `command_name`: `agent`
- `fixed_args`: `["--model", "composer-2.5"]`

The daemon starts the process directly with `exec.Command(command_name,
fixed_args...)`; it does not run a shell.

Supported input:

- plain arguments separated by whitespace
- single or double quotes for values with spaces
- backslash escaping for literal spaces or quote characters

The UI parser is argv-oriented, not a full POSIX shell. Inside double quotes,
`\` escapes the next character directly; use single quotes when you need `$` or
backticks to stay literal. Running tasks keep the launch args they started with;
profile command or argument edits apply to newly claimed tasks after the daemon
re-registers.

Unsupported input:

- pipes, redirects, `;`, `&&`, `||`
- backticks
- `$VAR` or `$(...)` expansion

Use a wrapper script when the runtime needs shell behavior.

## Command not found

Desktop-launched daemons may not inherit the same `PATH` as an interactive
terminal. If a custom runtime shows a registration error even though the command
works in your shell, pin the absolute path on that machine:

```sh
multica runtime profile set-path <profile-id> --path /abs/path/to/agent
```

Then restart or refresh the daemon so it re-registers the profile.

## Upgrade order

Custom runtime arguments and registration-error reporting require both the
server and daemon versions that support `fixed_args` launch specs and
`failed_profiles` registration reports. In mixed deployments, upgrade the server
before rolling out newer daemons so failed custom-only profiles can be recorded
instead of being rejected as an empty runtime registration.
