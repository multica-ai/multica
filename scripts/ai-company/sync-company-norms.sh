#!/usr/bin/env bash
# Copy selected .ai-company/ norm docs into portfolio repos (.delivery/company-os/).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
MANIFEST="${MANIFEST:-$MULTICA_ROOT/.ai-company/config/company-os-sync-manifest.yaml}"
DRY_RUN=0
INCLUDE_PAUSED=0
SYNC_HARNESS=0
FORCE_HARNESS=0
PROJECT_ID=""

usage() {
  cat <<'EOF'
Usage: sync-company-norms.sh [options]

Copy norm docs from multica/.ai-company/ into each portfolio checkout at
.delivery/company-os/ (manifest: .ai-company/config/company-os-sync-manifest.yaml).

Options:
  --id PROJECT_ID       Sync one registry project only
  --registry PATH       project-registry.yaml
  --manifest PATH       company-os-sync-manifest.yaml
  --include-paused      Include paused projects
  --harness             Also run install-harness.sh (refresh agents/workflows)
  --force-harness       Pass --force to install-harness.sh
  --dry-run             Print actions only
  -h, --help

After sync, commit in each product repo:
  git add .delivery/company-os .delivery/COMPANY-OS.md
  git commit -m "chore: sync company-os norms from multica"

See .ai-company/docs/27-norm-sync.md
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --id) PROJECT_ID="${2:?}"; shift 2 ;;
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --manifest) MANIFEST="${2:?}"; shift 2 ;;
    --include-paused) INCLUDE_PAUSED=1; shift ;;
    --harness) SYNC_HARNESS=1; shift ;;
    --force-harness) SYNC_HARNESS=1; FORCE_HARNESS=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [ ! -f "$MANIFEST" ]; then
  echo "error: manifest not found: $MANIFEST" >&2
  exit 1
fi

hq_sha="$(git -C "$MULTICA_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
synced_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

project_ids="$(
  python3 - "$REGISTRY" "$PROJECT_ID" "$INCLUDE_PAUSED" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
only_id = sys.argv[2]
include_paused = sys.argv[3] == "1"
ids: list[str] = []
current: dict[str, str] = {}

def flush():
    global current
    if not current.get("id"):
        return
    if only_id and current["id"] != only_id:
        current = {}
        return
    if current.get("paused") == "true" and not include_paused:
        current = {}
        return
    ids.append(current["id"])
    current = {}

for line in registry.read_text(encoding="utf-8").splitlines():
    s = line.strip()
    if s.startswith("- id:"):
        flush()
        current = {"id": s.split(":", 1)[1].strip()}
        continue
    if not current:
        continue
    if s.startswith("paused:"):
        current["paused"] = s.split(":", 1)[1].strip()

flush()
for pid in ids:
    print(pid)
PY
)"

if [ -z "$project_ids" ]; then
  echo "sync-company-norms: no matching projects in registry" >&2
  exit 1
fi

ok=0
skip=0
fail=0

run_cmd() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

sync_docs_to_repo() {
  local target="$1"
  local dest="$target/.delivery/company-os"

  python3 - "$MULTICA_ROOT" "$MANIFEST" "$dest" "$hq_sha" "$synced_at" "$DRY_RUN" <<'PY'
import shutil
import sys
from datetime import datetime
from pathlib import Path

root = Path(sys.argv[1])
manifest = Path(sys.argv[2])
dest = Path(sys.argv[3])
hq_sha = sys.argv[4]
synced_at = sys.argv[5]
dry_run = sys.argv[6] == "1"

lines = manifest.read_text(encoding="utf-8").splitlines()
paths: list[str] = []
in_paths = False
for line in lines:
    s = line.strip()
    if s.startswith("paths:"):
        in_paths = True
        continue
    if in_paths:
        if not s.startswith("- "):
            if s and not s.startswith("#"):
                break
            continue
        rel = s[2:].strip().strip('"').strip("'")
        if rel and not rel.startswith("#"):
            paths.append(rel)

copied: list[str] = []
missing: list[str] = []
for rel in paths:
    src = root / ".ai-company" / rel
    out = dest / rel
    if not src.is_file():
        missing.append(rel)
        continue
    if dry_run:
        print(f"[dry-run] copy {src} -> {out}")
    else:
        out.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, out)
    copied.append(rel)

readme = dest / "README.md"
body = [
    "# Company OS snapshot (product copy)\n",
    f"> synced: {synced_at} · multica @ `{hq_sha}`\n",
    "\n",
    "Authoritative source: multica fork `.ai-company/` on the CEO machine.\n",
    "Refresh:\n",
    "\n",
    "```bash\n",
    f"bash {root}/scripts/ai-company/sync-company-norms.sh --id <project-id>\n",
    "```\n",
    "\n",
    "## Files in this snapshot\n",
    "\n",
]
for rel in copied:
    body.append(f"- `{rel}`\n")
if missing:
    body.append("\n## Missing at sync time (check manifest)\n\n")
    for rel in missing:
        body.append(f"- `{rel}`\n")

if dry_run:
    print(f"[dry-run] write {readme} ({len(copied)} files)")
else:
    readme.parent.mkdir(parents=True, exist_ok=True)
    readme.write_text("".join(body), encoding="utf-8")

print(f"docs:{len(copied)} missing:{len(missing)}")
PY
}

write_company_os_pointer() {
  local target="$1"
  local pointer="$target/.delivery/COMPANY-OS.md"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] write $pointer"
    return 0
  fi
  cat >"$pointer" <<EOF
# Company OS pointer

**Read norms in this repo first:** \`.delivery/company-os/README.md\`

Snapshot synced from multica \`${hq_sha}\` at \`${synced_at}\`.

| Layer | Location |
|-------|----------|
| **Norms (this repo)** | \`.delivery/company-os/\` |
| **HQ truth (CEO machine)** | multica \`.ai-company/\` |
| **Execution harness** | \`.delivery/\` · \`.cursor/agents/\` · workflows |

Refresh norms:

\`\`\`bash
bash /path/to/multica/scripts/ai-company/sync-company-norms.sh --id <project-id>
\`\`\`

Full playbook: \`.delivery/company-os/docs/27-norm-sync.md\` (after sync).
EOF
}

while IFS= read -r pid; do
  [ -n "$pid" ] || continue
  echo ">> $pid"
  if ! path="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --id "$pid" --quiet 2>/dev/null)"; then
    echo "   skip: no local checkout (set AI_REPO_PATH_* in local.env)" >&2
    skip=$((skip + 1))
    continue
  fi
  if [ ! -d "$path/.git" ]; then
    echo "   skip: not a git repo: $path" >&2
    skip=$((skip + 1))
    continue
  fi

  if [ "$SYNC_HARNESS" -eq 1 ]; then
    harness_args=()
    [ "$FORCE_HARNESS" -eq 1 ] && harness_args+=(--force)
    [ "$DRY_RUN" -eq 1 ] && harness_args+=(--dry-run)
    if ! run_cmd bash "$MULTICA_ROOT/.ai-company/harness/install.sh" ${harness_args+"${harness_args[@]}"} "$path"; then
      echo "   warn: harness install failed" >&2
    fi
  elif [ ! -d "$path/.delivery" ]; then
    echo "   warn: no .delivery/ — run with --harness or install-harness.sh first" >&2
  fi

  if ! sync_docs_to_repo "$path"; then
    echo "   fail: doc sync" >&2
    fail=$((fail + 1))
    continue
  fi
  write_company_os_pointer "$path"
  echo "   ok: $path"
  ok=$((ok + 1))
done <<<"$project_ids"

echo ""
echo "sync-company-norms: ok=$ok skip=$skip fail=$fail dry_run=$DRY_RUN"
[ "$fail" -eq 0 ] || exit 1
