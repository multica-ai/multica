#!/bin/sh
set -eu

# The runtime is trusted orchestration code, but its HTTP server still runs as
# the unprivileged node user. Match the host Docker socket's group at startup so
# only the launcher can create the short-lived, locked-down app containers.
if [ -S /var/run/docker.sock ]; then
  socket_gid="$(stat -c '%g' /var/run/docker.sock)"
  exec su-exec node:"$socket_gid" "$@"
fi

exec su-exec node "$@"
