#!/usr/bin/env bash
# Runs this checkout's `make up` environment as a daemon-supervised environment
# declared in .multica/environments.toml.
#
#   agent-environment.sh start <components>   # e.g. api,web
#   agent-environment.sh stop
#
# The two models differ in two ways, and this script is the adapter:
#
#  1. The daemon hands out $MULTICA_PORT and reports http://localhost:$MULTICA_PORT.
#     dev-env.sh allocates its own ports, and the web app's origin, API URL and
#     CORS settings are built from them. So the web app keeps its dev-env port,
#     and $MULTICA_PORT answers with a redirect to it.
#  2. The daemon treats an environment as alive while the start command's
#     process group is. dev-env.sh detaches every component into its own group
#     and returns. So `start` stays in the foreground after `up`, and exits once
#     the web app is gone, which is what lets the sidebar report it as exited.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV_ENV="$REPO_ROOT/scripts/dev-env.sh"

# One hour longer than the ttl in environments.toml: the daemon's stop runs
# first, and dev-env's own expiry only collects what a dead daemon left behind.
DEV_ENV_TTL_HOURS=25

cmd_start() {
  local components="${1:-api,web}" status
  : "${MULTICA_PORT:?MULTICA_PORT is not set; start this through 'multica env start <name>'.}"

  bash "$DEV_ENV" up --components "$components" --ttl "$DEV_ENV_TTL_HOURS"
  status="$(bash "$DEV_ENV" status --json)"

  exec node -e '
    const http = require("node:http");
    const net = require("node:net");

    const status = JSON.parse(process.argv[1]);
    const port = Number(process.env.MULTICA_PORT);
    const target = `http://localhost:${status.frontend_port}`;
    const home = `/${status.workspace_slug}/issues`;

    const redirect = (req, res) => {
      res.writeHead(302, { Location: target + (req.url === "/" ? home : req.url) });
      res.end();
    };
    http.createServer(redirect).listen(port, "127.0.0.1");
    // Best effort: a machine without IPv6 loopback still has the listener above.
    http.createServer(redirect).on("error", () => {}).listen(port, "::1");
    console.log(`http://localhost:${port} redirects to ${target}${home}`);

    let misses = 0;
    setInterval(() => {
      const socket = net.connect({ port: status.frontend_port, host: "localhost" });
      socket.setTimeout(3000);
      socket.once("connect", () => { misses = 0; socket.destroy(); });
      const miss = () => {
        socket.destroy();
        if (++misses < 3) return;
        console.error(`web app on :${status.frontend_port} is gone; exiting.`);
        process.exit(1);
      };
      socket.once("error", miss);
      socket.once("timeout", miss);
    }, 5000);
  ' "$status"
}

cmd_stop() {
  # A start that failed before registering leaves nothing to destroy.
  if ! bash "$DEV_ENV" status --json >/dev/null 2>&1; then
    echo "No environment registered for $REPO_ROOT; nothing to stop."
    return 0
  fi
  bash "$DEV_ENV" destroy --yes
}

case "${1:-}" in
  start) shift; cmd_start "$@" ;;
  stop) cmd_stop ;;
  *) echo "Usage: agent-environment.sh start <components> | stop" >&2; exit 2 ;;
esac
