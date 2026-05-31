# Packaging

Operator-facing artifacts that live outside the application code: launchd
agents, install scripts, distribution packaging.

## Local token sync (macOS)

`multica-token-sync` is a tiny launchd agent that follows the cluster-side
Claude OAuth broker as the authoritative writer for the refresh chain. The
broker rotates tokens in the cluster; without this tool, the local Keychain
goes stale every time the broker refreshes, and the local `claude` CLI breaks
until you run `claude /login` again.

The agent polls the broker's state Secret every 30 minutes, transforms the
bytes into the JSON shape Claude Code's macOS Keychain entry expects, and
upserts the entry. The broker becomes the single writer, your laptop is a
read-only follower — `/login` ceremonies disappear after the initial bootstrap.

### Prerequisites

- macOS.
- `kubectl` configured against the cluster with `get` permission on
  `secrets/multica-claude-oauth-broker` in the broker's namespace (default
  `multica`). If `kubectl -n multica get secret multica-claude-oauth-broker`
  works, so will this tool.
- Go 1.26+ to build.

### Install

```bash
cd server && go build -o /tmp/multica-token-sync ./cmd/multica-token-sync
sudo install -m 0755 /tmp/multica-token-sync /usr/local/bin/multica-token-sync
./packaging/launchd/install.sh install
```

The installer copies `com.multica.token-sync.plist` to
`~/Library/LaunchAgents/`, rewrites the `__USER_HOME__` placeholder, and runs
`launchctl bootstrap`. The first sync fires immediately (`RunAtLoad`); the
ticker then runs every 1800s.

### Verify

```bash
./packaging/launchd/install.sh status        # launchd state
tail -f ~/Library/Logs/multica-token-sync.log
```

Expected log on success:
```
INFO msg="keychain updated" service="Claude Code-credentials" account=<you> expires_at=…
```
or
```
INFO msg="keychain already current" fingerprint=…
```

### Manual force-sync

```bash
multica-token-sync --once --verbose
multica-token-sync --dry-run --verbose      # diff without writing
```

### Uninstall

```bash
./packaging/launchd/install.sh uninstall
sudo rm /usr/local/bin/multica-token-sync   # optional
```

### Caveat

A long-running interactive `claude` session holds tokens in memory; a broker
rotation that happens mid-session takes effect at the *next* CLI invocation,
not in-flight. This is the same behavior you'd see if you ran `claude /login`
mid-session — there's nothing the sync tool can do about it.
