# Tencent Gongfeng

The first integration targets the official service at `https://git.code.tencent.com`.
Git checkout and push use the runtime host's Git credentials. MR commands use
[gongfeng-cli](https://github.com/studyzy/gongfeng-cli), a community CLI; consult
`gongfeng --help` and the relevant subcommand's `--help` before use.

## Authentication and reads

The runtime operator installs a pinned CLI revision and authenticates it. It
accepts `GONGFENG_TOKEN` or `~/.gongfeng.json` (plaintext; use mode `0600` on
macOS/Linux or a personal file ACL on Windows). Some agents filter environment
variables, so confirm authentication inside the actual agent runtime. Never put a token in command arguments,
repository files, or task comments. The workspace's encrypted integration token is only
used by the server and is not delivered to the runtime. Missing CLI or account
access is a prerequisite to report, not a reason to claim success.

Always pass `--base-url https://git.code.tencent.com` to API commands. Repository
configuration can override the home token and API address; stop if a local
`.gongfeng.json` changes credentials or the endpoint unexpectedly. The explicit
URL overrides local endpoint configuration, but it does not isolate credentials
or restrict HTTP redirects.

Before writes, verify the authenticated account matches the runtime operator's
authorized identity:

```sh
gongfeng --base-url https://git.code.tencent.com user me --json
gongfeng --project-id <namespace/project> --base-url https://git.code.tencent.com mr list --json
gongfeng --project-id <namespace/project> --base-url https://git.code.tencent.com mr show <global-mr-id> --json
```

Starting a Multica task does not select the creator's Gongfeng account. Separate
Multica profiles under the same OS account still share the home CLI credentials.
Use separate personal runtime environments and credentials for personal MR
attribution. Git commit authors and SSH push identities are configured separately;
do not rewrite shared or global Git identity to impersonate a task creator.

The CLI's `show`, `update`, and `accept` commands use the global MR `id` returned
by the API. Multica's card number and the repository MR URL use `iid`. Read the
list response to resolve the global id; do not pass the card number as an id.

## Creating an MR

Push the task branch using Git, then create the MR in that repository:

```sh
gongfeng --project-id <namespace/project> mr create \
  --base-url https://git.code.tencent.com \
  --source-branch <task-branch> \
  --target-branch <target-branch> \
  --title "<issue-identifier> Short title" \
  --description "Closes <issue-identifier>"
```

Resolve the target branch from the project's starting branch or an existing MR.
A tag or commit is only a starting point; confirm the target branch. Keep an
existing MR's target on resumed work. Include a title/branch issue identifier
or a closing body reference to link the MR; bare body mentions do not link.
Read `issues.md` for the shared merge/status contract. Multica does not create
MRs or merge code on the agent's behalf. The server refreshes CI status for
linked MRs; runtime CLI credentials remain separate from the server token.

For commit-check writes, the official API accepts `pending`, `success`, `error`,
and `failure`. The pinned community CLI's help lists other state names; use the
official values with `gongfeng commit-status create --state`.
Always pass the official `--base-url` for check writes as well. Multica refreshes
checks but does not execute the repository's tests. Continuous check results
require a configured CI runner that reports against the current source SHA.
