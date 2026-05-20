# Local runtime deploy scripts

Everything in this folder runs the Firtal-cerebro stack on a single Mac (today
that's `sara.tailbde0.ts.net`). It is *not* shared with upstream Multica — the
local-runtime model is a cerebro-specific choice. Plist files in this directory
are templates; the live copies in `~/Library/LaunchAgents/` are installed
manually.

## Log rotation

Every launchd-managed service sources `_log-rotation.sh` before it `exec`s
into the actual binary. That helper pipes `stdout`/`stderr` through
`log-rotator.py`, which writes to `.deploy/logs/<service>.{out,err}.log` and
rotates by size.

**Policy:** 100 MB per file, 7 numbered generations kept
(`backend.err.log`, `backend.err.log.1` … `backend.err.log.7`). Older drops
off. Effective on-disk ceiling per stream: ~700 MB. Time-based retention is
not enforced — at typical sara-runtime volume (backend.err.log was filling
~3 GB / week before rotation) seven 100 MB generations is roughly two days
of history, which is fine for post-deploy debugging.

The rotator also rotates on startup if the existing file is already over
the threshold. That means an oversize log (e.g. the 3.4 GB `backend.err.log`
that triggered JEH-1881) gets archived to `.1` the next time the service
restarts — no manual `truncate` needed.

### Files

- `log-rotator.py` — tiny Python 3 rotator. Stdin → file, with size-based
  rotation. Argparse defaults match the policy above (override with
  `--max-bytes` / `--keep`).
- `_log-rotation.sh` — bash helper, sourced by each `run-*.sh`. Sets up
  the process-substitution redirection. Requires `$REPO` and `$LOG_NAME`
  to be defined before sourcing.
- `run-backend.sh` / `run-daemon.sh` / `run-frontend.sh` / `run-webhook.sh`
  — launchd entry points. Each sources `_log-rotation.sh` and then `exec`s
  the service binary.

### Verifying rotation in production

```bash
# Confirm rotator is in the process tree for a service
ps -ef | grep -E "log-rotator.*$(echo backend)\\.(out|err)" | grep -v grep

# Watch sizes — current file should stay under 100 MB
ls -la /Users/sara/code/firtal-cerebro/.deploy/logs/backend.*

# Force a rotation by writing >100 MB through a service (not recommended
# in prod — usually you just wait for natural traffic).
```

### Adding rotation to a new service

1. Make sure the service uses a `run-<name>.sh` script invoked by launchd
   (not a direct binary exec from the plist).
2. In the new script, before any `exec`:

   ```bash
   REPO=/Users/sara/code/firtal-cerebro
   LOG_NAME=<name>
   # shellcheck source=_log-rotation.sh
   source "$REPO/.deploy/_log-rotation.sh"
   ```

3. Point the plist's `StandardOutPath` / `StandardErrorPath` at
   `.deploy/logs/<name>.out.log` / `.err.log` (used for any output before
   the redirection takes effect — typically empty).
4. Restart the service: `launchctl kickstart -k gui/$(id -u)/<label>`.

### Why not `newsyslog`?

macOS ships `newsyslog` and runs it hourly via launchd, and dropping a
config in `/etc/newsyslog.d/` would also work. We chose the in-repo
rotator because:

- It follows the repository — fresh installs and upstream syncs get
  rotation for free, no out-of-band `/etc/` edit.
- No `sudo` needed at install time.
- Same behaviour on any developer machine, not just sara-runtime.

## Backend port-wait

`run-backend.sh` polls TCP port `$PORT` (default 8180) for up to 3 s
before `exec`ing the server. This avoids the brief "address already in
use" line that used to appear in `backend.err.log` whenever
`launchctl kickstart -k` restarted the backend before the OS had
released the previous instance's listening socket. KeepAlive=true would
respawn the failed first attempt within a second or two, but the noise
was real. See JEH-1881.
