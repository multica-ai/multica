#!/usr/bin/env bash
# Runs this checkout's `make up` environment as a daemon-supervised environment
# declared in .multica/environments.toml.
#
#   agent-environment.sh start web|desktop
#   agent-environment.sh stop  web|desktop
#
# The two models differ in two ways, and this script is the adapter:
#
#  1. The daemon hands out $MULTICA_PORT and reports http://localhost:$MULTICA_PORT.
#     dev-env.sh allocates its own ports, and the web app's origin, API URL and
#     CORS settings are built from them. So the web app keeps its dev-env port,
#     and $MULTICA_PORT answers with a redirect to it. Desktop has no URL;
#     $MULTICA_PORT only answers once `up` has finished, which is what the
#     environment's ready check waits for.
#  2. The daemon treats an environment as alive while the start command's
#     process group is. dev-env.sh detaches every component into its own group
#     and returns. So `start` stays in the foreground after `up`, and exits once
#     the component is gone, which is what lets the sidebar report it as exited.
#     It cleans up on its way out, because once it has exited the daemon
#     neither runs `stop` nor expires the environment.
#
# web and desktop share one dev-env environment: the same API, database and
# profile. Each running `start` holds a claim on it. Stopping a component, or
# its `start` exiting, gives up the claim; whoever gives up the last one
# destroys the environment. Claims are read and changed under one lock per
# checkout, so two stops at once still leave exactly one of them to destroy.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV_ENV="$REPO_ROOT/scripts/dev-env.sh"
STATE_DIR="$REPO_ROOT/.multica/agent-environments"
LOCK_DIR="$STATE_DIR/lock.d"

# One hour longer than the ttl in environments.toml: the daemon's stop runs
# first, and dev-env's own expiry only collects what a dead daemon left behind.
DEV_ENV_TTL_HOURS=25

usage() {
  echo "Usage: agent-environment.sh start|stop web|desktop" >&2
  exit 2
}

other_component() {
  case "$1" in
    web) printf 'desktop' ;;
    desktop) printf 'web' ;;
    *) usage ;;
  esac
}

# An atomic mkdir, as in dev-env.sh: flock is not on a stock macOS. A lock
# whose holder is gone is taken over, so a killed stop cannot wedge the next.
acquire_lock() {
  local holder waited=0
  mkdir -p "$STATE_DIR"
  while ! mkdir "$LOCK_DIR" 2>/dev/null; do
    holder="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
    if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
      rm -rf "$LOCK_DIR"
      continue
    fi
    waited=$((waited + 1))
    [ "$waited" -lt 3000 ] || { echo "Timed out waiting for $LOCK_DIR." >&2; exit 1; }
    sleep 0.1
  done
  printf '%s\n' "$$" > "$LOCK_DIR/pid"
}

release_lock() { rm -rf "$LOCK_DIR"; }

claim_file() { printf '%s/%s.claim' "$STATE_DIR" "$1"; }

# The pid of the `start` holding a claim on the component, if it still runs.
# A claim left by a `start` that was killed outright is not one.
claim_holder() {
  local pid
  pid="$(cat "$(claim_file "$1")" 2>/dev/null || true)"
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && printf '%s' "$pid"
  return 0
}

# Stops the component, and the whole environment when nothing else claims it.
# The caller holds the lock and has given up the component's claim.
release_component() {
  local component="$1" other
  other="$(other_component "$component")"
  if ! bash "$DEV_ENV" status --json >/dev/null 2>&1; then
    echo "No environment registered for $REPO_ROOT; nothing to stop."
    return 0
  fi
  bash "$DEV_ENV" down --components "$component"
  if [ -n "$(claim_holder "$other")" ]; then
    echo "$other still uses the API and database; kept them."
  else
    bash "$DEV_ENV" destroy --yes
  fi
}

# Runs when `start` exits for any reason: the component went away, `up`
# failed, or the daemon signalled it after running `stop`. Only a claim this
# process still holds is given up; one `stop` already took is left alone.
finish_start() {
  local component="$1" status="$2"
  trap - EXIT
  acquire_lock
  if [ "$(claim_holder "$component")" = "$$" ]; then
    rm -f "$(claim_file "$component")"
    release_component "$component" || true
  fi
  release_lock
  exit "$status"
}

cmd_start() {
  local component="$1" status
  other_component "$component" >/dev/null
  : "${MULTICA_PORT:?MULTICA_PORT is not set; start this through 'multica env start <name>'.}"

  # Electron listens on nothing by itself. The DevTools port is what lets the
  # daemon find the app among this environment's processes and offer its
  # window in the sidebar, and it is what scripts/desktop-cdp.mjs attaches to.
  local flags=()
  [ "$component" != desktop ] || flags+=(--cdp)

  acquire_lock
  printf '%s\n' "$$" > "$(claim_file "$component")"
  release_lock
  trap "finish_start $component \$?" EXIT

  bash "$DEV_ENV" up --components "api,$component" --ttl "$DEV_ENV_TTL_HOURS" ${flags[@]+"${flags[@]}"}
  status="$(bash "$DEV_ENV" status --json)"

  node -e '
    const http = require("node:http");
    const net = require("node:net");

    const status = JSON.parse(process.argv[1]);
    const component = process.argv[2];
    const port = Number(process.env.MULTICA_PORT);
    // The port whose listener is the component: Next for web, the renderer
    // dev server for desktop, which goes away when the app is quit.
    const watched = component === "web" ? status.frontend_port : status.desktop_renderer_port;
    const target = `http://localhost:${status.frontend_port}`;
    const home = `/${status.workspace_slug}/issues`;

    const answer = (req, res) => {
      if (component === "web") {
        res.writeHead(302, { Location: target + (req.url === "/" ? home : req.url) });
        res.end();
      } else {
        res.writeHead(200, { "Content-Type": "text/plain" });
        res.end("The Desktop app of this environment is running.\n");
      }
    };
    http.createServer(answer).listen(port, "127.0.0.1");
    // Best effort: a machine without IPv6 loopback still has the listener above.
    http.createServer(answer).on("error", () => {}).listen(port, "::1");
    console.log(component === "web"
      ? `http://localhost:${port} redirects to ${target}${home}`
      : `desktop is up; watching its renderer on :${watched}`);

    let misses = 0;
    setInterval(() => {
      const socket = net.connect({ port: watched, host: "localhost" });
      socket.setTimeout(3000);
      socket.once("connect", () => { misses = 0; socket.destroy(); });
      const miss = () => {
        socket.destroy();
        if (++misses < 3) return;
        console.error(`${component} on :${watched} is gone; exiting.`);
        process.exit(1);
      };
      socket.once("error", miss);
      socket.once("timeout", miss);
    }, 5000);
  ' "$status" "$component"
}

cmd_stop() {
  local component="$1"
  other_component "$component" >/dev/null
  acquire_lock
  trap release_lock EXIT
  rm -f "$(claim_file "$component")"
  release_component "$component"
}

[ $# -eq 2 ] || usage
case "$1" in
  start) cmd_start "$2" ;;
  stop) cmd_stop "$2" ;;
  *) usage ;;
esac
