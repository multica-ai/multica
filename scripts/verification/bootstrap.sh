#!/usr/bin/env bash
set -euo pipefail

verify_repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
verify_tools_root="${MULTICA_VERIFICATION_TOOLS_ROOT:-$HOME/.multica/verification-tools}"
mkdir -p "$verify_tools_root"
chmod 700 "$verify_tools_root"
if [ "$(uname -s)" != "Darwin" ]; then
  echo "The first verification bootstrap supports macOS." >&2
  exit 1
fi
case "$(uname -m)" in arm64) verify_arch=arm64 ;; x86_64) verify_arch=x64 ;; *) echo "Unsupported Mac architecture." >&2; exit 1 ;; esac
if [ ! -x "$verify_tools_root/node/bin/node" ]; then
  verify_temporary="$(mktemp -d "$verify_tools_root/.bootstrap-XXXXXX")"
  trap 'rm -rf "$verify_temporary"' EXIT
  curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 https://nodejs.org/dist/latest-v24.x/SHASUMS256.txt > "$verify_temporary/SHASUMS256.txt"
  verify_archive="$(awk -v arch="$verify_arch" '$2 ~ ("^node-v[0-9]+\\.[0-9]+\\.[0-9]+-darwin-" arch "\\.tar\\.gz$") {print $2; exit}' "$verify_temporary/SHASUMS256.txt")"
  if [ -z "$verify_archive" ]; then echo "No matching archive in the official Node manifest." >&2; exit 1; fi
  curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 "https://nodejs.org/dist/latest-v24.x/$verify_archive" > "$verify_temporary/$verify_archive"
  (cd "$verify_temporary" && awk -v name="$verify_archive" '$2 == name' SHASUMS256.txt | shasum -a 256 -c -)
  mkdir "$verify_temporary/node"
  tar -xzf "$verify_temporary/$verify_archive" --strip-components=1 -C "$verify_temporary/node"
  mv "$verify_temporary/node" "$verify_tools_root/node"
fi
export PATH="$verify_tools_root/node/bin:$PATH"
verify_pnpm_version="$(node -e 'const p=require(process.argv[1]);const m=/^pnpm@(\d+\.\d+\.\d+)/.exec(p.packageManager);if(!m)process.exit(1);process.stdout.write(m[1]);' "$verify_repo_root/package.json")"
if [ ! -f "$verify_tools_root/pnpm/node_modules/pnpm/bin/pnpm.cjs" ] || [ "$(node "$verify_tools_root/pnpm/node_modules/pnpm/bin/pnpm.cjs" --version 2>/dev/null || true)" != "$verify_pnpm_version" ]; then
  npm install --prefix "$verify_tools_root/pnpm" --ignore-scripts --no-audit --no-fund "pnpm@$verify_pnpm_version"
fi
node "$verify_tools_root/pnpm/node_modules/pnpm/bin/pnpm.cjs" --manage-package-manager-versions=false --dir "$verify_repo_root" install --filter @multica/verification... --frozen-lockfile
node "$verify_tools_root/pnpm/node_modules/pnpm/bin/pnpm.cjs" --manage-package-manager-versions=false --dir "$verify_repo_root" --filter @multica/verification build:manual
echo "Verification tools prepared. Configure scripts/verification/mcp.sh as a stdio MCP command."
