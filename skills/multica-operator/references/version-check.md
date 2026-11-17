# Session Version Check

Run this check once in the current agent session, before the CLI setup or
connection workflow. It is an informational, non-blocking check: it must never
prevent the agent from continuing the original request.

## Check workflow

1. Before any network access, mark this check complete in current-session
   reasoning. Do not repeat it until a new agent session, even when the lookup
   fails.
2. Read the exact local `VERSION` file next to `SKILL.md`. If it is `dev`, empty,
   malformed, or otherwise indicates a local development state, stay silent and
   continue the original request.
3. Fetch the stable Marketplace JSON endpoint without credentials:
   `https://raw.githubusercontent.com/multica-ai/multica/marketplace/release.json`.
4. Parse `version` and `release_url` as structured JSON. Accept only a stable
   `version` in the form `X.Y.Z`; `release_url` is the immutable release URL.
5. Compare the numeric, dot-separated components of the installed version and
   available version. Do not use lexical ordering (`1.10.0` is newer than
   `1.9.0`).
6. For an installed version that is current, equal, or newer, or for malformed
   local or remote data, an unsupported host, or a network failure, stay silent
   and continue the original request.
7. Only when the available version is newer, show the warning below once, then
   continue under the already-loaded instructions.

These are read-only examples for the active agent. They are not updater
scripts and must not write state.

First, resolve the directory containing the loaded `SKILL.md` and substitute
its absolute path for `<absolute-skill-directory>` in the commands below. Do
not run them with an unset shell or PowerShell variable; if the host runtime
cannot expose the loaded Skill path, use the explicit placeholder after resolving
the path from the loaded instructions.

POSIX shell:

```bash
installed_version=$(tr -d '\r\n' < "<absolute-skill-directory>/VERSION")
release_json=$(curl -fsSL \
  --connect-timeout 3 \
  --max-time 5 \
  https://raw.githubusercontent.com/multica-ai/multica/marketplace/release.json)
```

PowerShell:

```powershell
$installedVersion = (Get-Content -Raw (Join-Path '<absolute-skill-directory>' 'VERSION')).Trim()
$release = Invoke-RestMethod -TimeoutSec 5 -Uri 'https://raw.githubusercontent.com/multica-ai/multica/marketplace/release.json'
$availableVersion = $release.version
$releaseUrl = $release.release_url
```

After fetching, use a JSON parser available in the current host to read only
`version` and `release_url`; do not parse the response with string matching. Do
not send credentials, tokens, cookies, or authenticated request headers.

## Warning

Use a concise warning with all of these facts:

```text
A newer Multica Operator Skill is available (installed version: X.Y.Z;
available version: A.B.C; release URL: https://github.com/multica-ai/multica/releases/tag/vA.B.C).
Upgrade through the Marketplace that installed this Skill, or use the host's
native GitHub installation update workflow for a direct installation. The Skill
cannot infer its installation owner, so do not choose or execute an upgrade path
automatically. Marketplace availability varies by host, especially while a
Cursor version is under review. Start a new agent session after an upgrade; this
session continues under its already-loaded instructions.
```

Codex and Claude Code use their host-managed Marketplace upgrade actions.
Cursor can install only its latest approved submission. Native GitHub
installation is a separate host-owned path. Never claim that one channel owns
the current installation merely because another is available.

Do not download archives, extract files, cache release data, replace files, or
roll back files; do not modify the Skill directory. This reference only warns; it
does not update the Skill or hot-reload a new version into the active session.
