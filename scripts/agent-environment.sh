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
#
# web and desktop share one dev-env environment: the same API, database and
# profile. Stopping one leaves the other running; stopping the last destroys it.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV_ENV="$REPO_ROOT/scripts/dev-env.sh"

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

# Prints a field of `dev-env.sh status --json`, e.g. components.web.state.
status_field() {
  node -e '
    const value = process.argv[2].split(".").reduce((acc, key) => (acc == null ? acc : acc[key]), JSON.parse(process.argv[1]));
    process.stdout.write(value == null ? "" : String(value));
  ' "$1" "$2"
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
  bash "$DEV_ENV" up --components "api,$component" --ttl "$DEV_ENV_TTL_HOURS" ${flags[@]+"${flags[@]}"}
  status="$(bash "$DEV_ENV" status --json)"

  exec node -e '
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
  local component="$1" other status
  other="$(other_component "$component")"
  # A start that failed before registering leaves nothing to stop.
  if ! status="$(bash "$DEV_ENV" status --json 2>/dev/null)"; then
    echo "No environment registered for $REPO_ROOT; nothing to stop."
    return 0
  fi
  if [ "$(status_field "$status" "components.$other.state")" = stopped ]; then
    bash "$DEV_ENV" destroy --yes
  else
    echo "$other is still in use; stopping only $component."
    bash "$DEV_ENV" down --components "$component"
  fi
}

[ $# -eq 2 ] || usage
case "$1" in
  start) cmd_start "$2" ;;
  stop) cmd_stop "$2" ;;
  *) usage ;;
esac
