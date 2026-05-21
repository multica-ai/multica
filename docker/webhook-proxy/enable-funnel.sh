#!/bin/sh
# Enables Tailscale Funnel for webhook proxy on port 8443.
# Idempotent — safe to call repeatedly (used by healthcheck).
set -e

# Wait for Tailscale to be online
for i in $(seq 1 10); do
  tailscale status >/dev/null 2>&1 && break
  sleep 2
done

# Check if funnel is already active
if tailscale funnel status 2>&1 | grep -q "Funnel on.*8443"; then
  exit 0
fi

# Clear any stale foreground listener before enabling
tailscale funnel --https=8443 off 2>/dev/null || true
sleep 1
tailscale funnel --bg --https=8443 http://127.0.0.1:4901
echo "Funnel enabled on port 8443"
