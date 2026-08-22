# Setup And Connection

Use this workflow when Multica has not been configured in the current agent
host, or when authentication or workspace discovery fails. This Skill uses the
community CLI as released; it does not add or assume a private CLI command.

## 1. Check the CLI

Do not narrate repository instructions, Skill or reference reads, version
checks, CLI presence checks, or installer selection. A failed non-blocking
Skill version lookup must stay silent. Run:

```bash
multica version
```

An explicit request to connect, sign in, or set up Multica authorizes installing
the missing community CLI through an official installer without a separate
confirmation. On macOS, use Homebrew when it is available:

```bash
brew install multica-ai/tap/multica
```

On macOS without Homebrew, and on Linux or another supported POSIX host, use:

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
```

On Windows PowerShell, use:

```powershell
irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
```

After any installer exits, rerun `multica version`. A verified installation is
an internal setup step and needs no progress message. If the installer fails or
the command is still unavailable, report the observed failure and stop; never
infer success from the installer exit status alone. Do not silently install
software for a request that does not explicitly ask to connect, sign in, or set
up Multica.

The CLI installer does not install or update this Skill. Do not run
`multica update` automatically. If a required command is unavailable, report
the unsupported operation and point the user to the current community CLI
release or Multica Web.

## 2. Select the target

When the user supplied a profile or server, keep that exact context on every
command. The community CLI has no safe structured command for reporting the
final effective server or identity, so do not infer either from plain-text
configuration output.

For an existing local configuration, inspect only non-secret settings:

```bash
multica config show
```

Treat this as configured-state information, not proof of the effective server
when flags or environment variables may override it. Do not run `config show`
before `multica version` proves the CLI is available. For an explicitly supplied
server, use that exact target for the workspace check:

```bash
multica --server-url <server-url> workspace list --output json
```

If the user did not supply a target and no usable target is configured, ask
whether to use Multica Cloud or a self-hosted deployment. Do not run `setup` or
`login` before the user chooses. This deployment choice is material even when
the preceding CLI installation was automatic.

For Multica Cloud, use:

- Server: `https://api.multica.ai`
- Apps: `https://multica.ai`

For a self-hosted target, require the exact Server and Apps URLs instead of
deriving either one. Ask for them only after the user chooses self-hosted, when
the user explicitly wants a new deployment, or when the CLI reports that setup
is incomplete. Preserve an existing configured target or an explicit
user-supplied target without asking the same deployment question again.

## 3. Login and setup

Run login or setup only when the user explicitly asks to connect, set up, or
sign in and the deployment target is resolved. Before opening a browser, give
one concise notice: authentication may complete automatically when the browser
already has a valid Web session; credentials, verification codes, and consent
stay in that browser.

For an existing configured target:

```bash
multica --profile <name> --server-url <server-url> login
```

Omit `--profile` only when the default profile was intentionally selected. For
fresh Cloud setup:

```bash
multica --profile <name> --server-url https://api.multica.ai setup
```

For local self-hosted setup with default ports:

```bash
multica --profile <name> setup self-host
```

For remote private setup, require both exact URLs:

```bash
multica --profile <name> setup self-host \
  --server-url https://api.internal.example \
  --app-url https://app.internal.example
```

The community CLI may wait for workspace creation when login finds no workspace,
and it may choose a default during login. Do not narrate those internal branches
in advance, treat an opened browser as success, or fill a browser form for the
user. Surface only a required user interaction, a material choice, an observed
failure, or the final verified workspace result.

Never request passwords, verification codes, or tokens in chat, and never print
them or their prefixes. For token login, use the community CLI's prompt form rather
than placing a token in shell history.

## 4. Verify workspace access

Run the workspace command with the same profile and server context:

```bash
multica --profile <name> --server-url <server-url> workspace list --output json
```

Valid JSON is the evidence that the selected CLI context can reach Multica.
Do not rely only on process exit status. Interpret the result as follows:

- `[]` means the request succeeded but the account has no accessible workspace;
  report that state and ask whether the user wants to create one.
- With one workspace, show its display name and ask before switching if no
  default is selected.
- With multiple workspaces, show display names only and ask the user to choose
  before running `multica workspace switch <exact-slug>`. Do not choose the
  first entry in the Skill.
- On a network, TLS, or server error, report the observed CLI error and keep the
  current configuration unchanged.

If the user wants a workspace, ask for its display name and a confirmed
immutable slug before running:

```bash
multica workspace create --name <name> --slug <slug> --output json
multica workspace switch <slug>
```

Do not invent a name, silently add a suffix after a slug collision, or create
from a bare yes. Verify the CLI result of each command.

## 5. Report and inspect on demand

Because the community CLI does not provide safe structured identity status,
report only verified workspace information:

```text
Workspace: <workspace name>
```

If no workspace was selected, say `Workspace: not selected`. Do not include
unverified server details, email, account ID, workspace UUID, slug, or any
credential in the summary.

Do not fetch resources merely because connection succeeded. When requested, run
only the matching structured command in the same profile, server, and workspace
context:

```bash
multica issue list --output json
multica autopilot list --output json
multica project list --output json
multica label list --output json
multica agent list --output json
multica skill list --output json
multica squad list --output json
```

If a needed command or structured output is absent from the installed community
CLI, say so and direct the user to Multica Web. Do not read profile tokens or
call the API directly as a fallback.
