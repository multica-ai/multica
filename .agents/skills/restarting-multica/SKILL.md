---
name: restarting-multica
description: Use when restarting Multica frontend, backend, daemon, nginx, or the entire stack, or when IP access breaks after a restart attempt on the 3.5G RAM production server. Symptoms include 502 Bad Gateway, 401 invalid token, OOM during next build, next-server supervisor restart loop, or next dev crashes. Also use after git pull or make build to deploy new code.
---

# Restarting Multica

## Overview

Strict ordered procedure for restarting Multica services on the 3.5G RAM production server (47.102.103.66). Skipping steps or wrong order causes 502 (nginx), 401 (env not exported), OOM during build, or supervisor restart loops.

This is a **technique skill** — follow the steps in order. Do not improvise alternatives (dev mode, different binary path, different env loading) unless you have verified them.

## When to Use

- User asks to restart frontend, backend, daemon, nginx, or "the whole project"
- After `git pull` or `make build` to deploy new code
- When IP access is broken (502, 401, timeout, blank page)
- When a service is unresponsive or in a restart loop

## Critical Rules (DO NOT SKIP)

1. **Binary copy**: Compiled binary is `server/bin/server`. Runtime path is `data/bin/multica-server`. Must copy after `make build` or the backend runs stale code.
2. **Env export**: Use `set -a && source .env && set +a`. Bare `source .env` does NOT export `JWT_SECRET` — every API request returns 401 "invalid token".
3. **Frontend = standalone, NEVER dev**: `pnpm dev` OOMs on 3.5G server (compile peak 1.7G, killed repeatedly). Must `STANDALONE=true pnpm --filter @multica/web build` then run `node server.js` with 128MB heap. Standalone is stable — no supervisor needed.
4. **Build prerequisites**: Before standalone build, set `vm.overcommit_memory=1` (default 0 blocks the 21G virtual memory allocation and silently OOM-kills the build), ensure 4G+ swap, kill memory-hungry processes (puppeteer/chrome), and stop backend to free RAM.
5. **Stop order**: frontend → backend → nginx (reverse of start).
6. **Start order**: backend → frontend → nginx.
7. **Verify after each step** before moving to the next.

## Pre-flight Checks

```bash
# What's currently running
ps aux | grep -E "multica|next-server|aa_nginx" | grep -v grep

# Memory state
free -h

# Listening ports
ss -tlnp 2>/dev/null | grep -E ":80|:8080|:3000"

# Confirm nginx proxy_temp_path fix is present (one-time, already in nginx.conf)
grep proxy_temp_path /home/admin/multica/nginx.conf
# Expected: proxy_temp_path /tmp/nginx-proxy-temp;
# If missing, add to http{} block and: mkdir -p /tmp/nginx-proxy-temp && chmod 700 /tmp/nginx-proxy-temp
```

## Full Restart Procedure

### Step 1: Stop services (in this exact order)

```bash
# 1a. Stop frontend — supervisor first (if running), then next processes
pkill -f "multica-web-supervisor" 2>/dev/null
sleep 1
pkill -f "next-server|next dev|pnpm --filter @multica/web" 2>/dev/null
sleep 1
pgrep -af "next-server|next dev" | grep -v grep   # must be empty

# 1b. Stop backend
pgrep -f "multica-server"                           # note the PID(s)
kill <backend-pid>
sleep 2
pgrep -f "multica-server" | grep -v $$              # must be empty

# 1c. Stop nginx (needs sudo)
sudo /usr/sbin/aa_nginx -s stop -c /home/admin/multica/nginx.conf 2>/dev/null
# If "pid file not found" error (pid file was lost), kill master directly:
#   pgrep -f "aa_nginx: master" → sudo kill <master-pid>
sleep 1
pgrep -af "aa_nginx" | grep -v grep                  # must be empty
```

### Step 2: Deploy new binary (skip if backend code unchanged)

```bash
cd /home/admin/multica
make build                                          # → server/bin/{server,multica,migrate}
cp server/bin/server data/bin/multica-server        # CRITICAL — runtime path
ls -la data/bin/multica-server server/bin/server    # sizes/mtimes must match
```

### Step 3: Build frontend standalone (skip if frontend code unchanged)

Build peaks at ~1.9G RAM. Prepare environment first or it will be OOM-killed silently.

```bash
# 3a. Kernel params — required, defaults block the build
sudo sysctl -w vm.overcommit_memory=1               # default 0 rejects 21G virtual memory
sudo sysctl -w vm.swappiness=100                    # default 0 prevents swap usage

# 3b. Ensure 4G+ swap
free -h | grep Swap
# If Swap < 4G, add temporary swap:
#   sudo fallocate -l 4G /swapfile2 && sudo chmod 600 /swapfile2 \
#     && sudo mkswap /swapfile2 && sudo swapon /swapfile2

# 3c. Free memory — kill puppeteer/chrome (they eat 250MB+)
pkill -f "puppeteer" 2>/dev/null
pkill -f "chrome-linux64/chrome" 2>/dev/null
# Backend already stopped in Step 1 — frees ~200MB more

# 3d. Build
cd /home/admin/multica
export STANDALONE=true
pnpm --filter @multica/web build 2>&1 | tee /tmp/build-output.log

# 3e. VERIFY standalone artifact exists — build silently OOM-kills if prereqs missed
ls -la apps/web/.next/standalone/apps/web/server.js
# If missing: check `sudo dmesg | grep "Out of memory"`, free more RAM, retry from 3d
```

### Step 4: Start backend

```bash
cd /home/admin/multica
set -a && source .env && set +a                     # CRITICAL — set -a exports, bare source does NOT
cd server
nohup /home/admin/multica/data/bin/multica-server > /tmp/go-server.log 2>&1 & disown
echo "backend PID: $!"

# Verify before continuing
for i in 1 2 3 4 5; do
  code=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health)
  [ "$code" = "200" ] && { echo "backend up: $code"; break; }
  sleep 1
done
```

### Step 5: Start frontend standalone

```bash
cd /home/admin/multica/apps/web/.next/standalone/apps/web
PORT=3000 HOSTNAME=127.0.0.1 NODE_OPTIONS="--max-old-space-size=128" \
  nohup node server.js > /tmp/nextjs-standalone.log 2>&1 & disown
echo "frontend PID: $!"

# Verify
for i in 1 2 3 4 5; do
  code=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:3000)
  [ "$code" = "200" ] && { echo "frontend up: $code"; break; }
  sleep 1
done
```

### Step 6: Start nginx

```bash
sudo /usr/sbin/aa_nginx -c /home/admin/multica/nginx.conf
sleep 1
pgrep -af "aa_nginx" | head -3                     # verify master + worker running
```

### Step 7: Final verification — all must pass

```bash
# Direct health checks
curl -s -o /dev/null -w "backend  8080/health: %{http_code}\n" http://localhost:8080/health
curl -s -o /dev/null -w "frontend 3000:       %{http_code}\n" http://localhost:3000
curl -s -o /dev/null -w "nginx    80:         %{http_code}\n" http://localhost:80

# Through nginx — root path MUST be 200 (not 502)
curl -s -o /dev/null -w "/            → %{http_code}\n" http://localhost/
curl -s -o /dev/null -w "/api/issues  → %{http_code}\n" http://localhost/api/issues

# nginx error log — no NEW errors after restart
tail -5 /tmp/nginx-error.log
```

Expected: backend 200, frontend 200, nginx 200, `/` 200. `/api/issues` may be 401 (auth required) — that's fine, means nginx reached backend. If `/api/issues` is 502, backend isn't up. If 401 everywhere including health, env wasn't exported.

## Daemon Restart (separate from web stack)

The daemon runs agent tasks (codex/claude) and uses the CLI binary `server/bin/multica`. Restart it independently when CLI code changes:

```bash
cd /home/admin/multica/server
./bin/multica daemon status       # current state
./bin/multica daemon restart      # stop + start with new binary
./bin/multica daemon status      # verify: "running", correct version
```

The daemon does not need the web stack running — it polls the cloud server URL from `~/.multica/config.json`.

## Common Mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| Bare `source .env` (no `set -a`) | All API requests 401 "invalid token" | Use `set -a && source .env && set +a` |
| `pnpm dev` for frontend | OOM killed, supervisor restart loop | Build standalone, run `node server.js` |
| `make build` but no binary copy | Backend runs old code | `cp server/bin/server data/bin/multica-server` |
| Build with `overcommit_memory=0` | Build silently OOM-killed, no standalone | `sudo sysctl -w vm.overcommit_memory=1` |
| Build with puppeteer/chrome running | OOM during build | `pkill -f puppeteer; pkill -f chrome-linux64/chrome` |
| Wrong start order (nginx before backend) | 502 on all requests | Start backend → frontend → nginx |
| Missing `proxy_temp_path` in nginx.conf | Intermittent 502 on larger API responses | `proxy_temp_path /tmp/nginx-proxy-temp;` in http{} block + `mkdir -p /tmp/nginx-proxy-temp && chmod 700 /tmp/nginx-proxy-temp` |
| Killing nginx without sudo | "Operation not permitted" | `sudo /usr/sbin/aa_nginx -s stop ...` |
| Forgetting `HOSTNAME=127.0.0.1` on frontend | Binds 0.0.0.0, bypasses nginx, external direct access | Always set `HOSTNAME=127.0.0.1` |

## Red Flags — STOP and Re-check

- nginx returns **502** → upstream not started, or wrong start order. Verify backend on 8080 first.
- API returns **401** everywhere → env not exported. Re-run with `set -a && source .env && set +a`.
- Frontend won't start, supervisor restart loop → using dev mode. Switch to standalone.
- `ls .next/standalone/apps/web/server.js` fails → build was OOM-killed. Check `sudo dmesg | grep "Out of memory"`, free RAM, retry build.
- `make build` succeeds but server still runs old code → forgot `cp server/bin/server data/bin/multica-server`.
- Next build "exits with code 0" in 9 lines of output but no standalone → OOM-killed mid-build. Check dmesg.
- Free memory < 1.5G before build → will OOM. Kill puppeteer/chrome, stop backend, add swap.

## Notes

- Kernel params (`overcommit_memory`, `swappiness`) and temporary swap are lost on reboot. Re-apply before next standalone build.
- The standalone build only needs to run when frontend code changes. If only backend changed, skip Step 3 entirely.
- The existing supervisor script `/tmp/multica-web-supervisor.sh` was for dev mode. Standalone is stable — do not start the supervisor.
- External access is via Nginx on port 80 → `http://47.102.103.66`. Aliyun security group must allow port 80.
