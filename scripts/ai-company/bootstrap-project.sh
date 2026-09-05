#!/usr/bin/env bash
# Bootstrap a product repo for the AI company: harness, GitHub repo, labels, optional backlog issues.
set -euo pipefail

MULTICA_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PROJECT_DIR=""
REPO=""
CREATE_REPO=0
PUSH=0
SYNC_BACKLOG=0
BACKLOG=""
TICKET_FROM=""
TICKET_TO=""
PRIVATE=1
DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: bootstrap-project.sh [options] PROJECT_DIR

One-shot setup for a new AI-company product repository.

Options:
  --repo OWNER/NAME       GitHub repo (default: infer from gh or dirname)
  --create-repo           Run gh repo create and set origin
  --public                Create public repo (default: private)
  --push                  git push after create-repo
  --sync-backlog          Create issues from backlog.md
  --backlog PATH          Backlog file (default: .delivery/<slug>/backlog.md)
  --from TICKET-NNN       Backlog sync start id
  --to TICKET-NNN         Backlog sync end id
  --dry-run               Print plan only
  -h, --help

Examples:
  bootstrap-project.sh ../music-game-sea \
    --repo your-org/music-game-sea \
    --create-repo --push \
    --sync-backlog --from TICKET-002 --to TICKET-007

  bootstrap-project.sh ../landing-tool-a \
    --create-repo --push \
    --sync-backlog --from TICKET-001 --to TICKET-004
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="${2:?}"; shift 2 ;;
    --create-repo) CREATE_REPO=1; shift ;;
    --public) PRIVATE=0; shift ;;
    --push) PUSH=1; shift ;;
    --sync-backlog) SYNC_BACKLOG=1; shift ;;
    --backlog) BACKLOG="${2:?}"; shift 2 ;;
    --from) TICKET_FROM="${2:?}"; shift 2 ;;
    --to) TICKET_TO="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) echo "Unknown option: $1" >&2; exit 1 ;;
    *)
      if [ -n "$PROJECT_DIR" ]; then
        echo "Unexpected argument: $1" >&2
        exit 1
      fi
      PROJECT_DIR="$1"
      shift
      ;;
  esac
done

if [ -z "$PROJECT_DIR" ]; then
  usage
  exit 1
fi

mkdir -p "$PROJECT_DIR"
PROJECT_DIR="$(cd "$PROJECT_DIR" && pwd)"
PROJECT_NAME="$(basename "$PROJECT_DIR")"

if [ -z "$REPO" ]; then
  REPO="your-org/$PROJECT_NAME"
fi

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

echo "== AI Company bootstrap =="
echo "  multica:  $MULTICA_ROOT"
echo "  project:  $PROJECT_DIR"
echo "  repo:     $REPO"
echo ""

# 1. Git init
if [ ! -d "$PROJECT_DIR/.git" ]; then
  run git -C "$PROJECT_DIR" init
fi

# 2. Harness (idempotent)
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[dry-run] bash $MULTICA_ROOT/.ai-company/harness/install.sh $PROJECT_DIR"
else
  bash "$MULTICA_ROOT/.ai-company/harness/install.sh" "$PROJECT_DIR"
fi

# 3. Default backlog path
if [ -z "$BACKLOG" ]; then
  if [ -f "$PROJECT_DIR/.delivery/$PROJECT_NAME/backlog.md" ]; then
    BACKLOG="$PROJECT_DIR/.delivery/$PROJECT_NAME/backlog.md"
  elif [ -f "$PROJECT_DIR/.delivery/music-game-sea/backlog.md" ]; then
    BACKLOG="$PROJECT_DIR/.delivery/music-game-sea/backlog.md"
  fi
fi

# 4. Initial commit if needed
if [ "$DRY_RUN" -eq 0 ] && [ -z "$(git -C "$PROJECT_DIR" status --porcelain 2>/dev/null)" ]; then
  echo "working tree clean — skip commit"
elif [ "$DRY_RUN" -eq 0 ]; then
  run git -C "$PROJECT_DIR" add -A
  run git -C "$PROJECT_DIR" commit -m "chore: bootstrap $PROJECT_NAME with AI company harness" || true
fi

# 5. GitHub repo
if [ "$CREATE_REPO" -eq 1 ]; then
  visibility="--private"
  [ "$PRIVATE" -eq 0 ] && visibility="--public"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] gh repo create $REPO $visibility --source=$PROJECT_DIR"
  else
    if ! gh repo view "$REPO" &>/dev/null; then
      gh repo create "$REPO" $visibility --source="$PROJECT_DIR" --remote=origin
    else
      echo "repo exists: $REPO"
      git -C "$PROJECT_DIR" remote add origin "https://github.com/$REPO.git" 2>/dev/null || \
        git -C "$PROJECT_DIR" remote set-url origin "https://github.com/$REPO.git"
    fi
  fi
fi

if [ "$PUSH" -eq 1 ]; then
  run git -C "$PROJECT_DIR" push -u origin HEAD
fi

# 6. Labels
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[dry-run] bash $MULTICA_ROOT/scripts/ai-company/ensure-github-labels.sh $REPO"
else
  bash "$MULTICA_ROOT/scripts/ai-company/ensure-github-labels.sh" "$REPO"
fi

# 7. Backlog → issues
if [ "$SYNC_BACKLOG" -eq 1 ]; then
  if [ -z "$BACKLOG" ] || [ ! -f "$BACKLOG" ]; then
    echo "warning: --sync-backlog but no backlog.md found" >&2
  else
    args=(--backlog "$BACKLOG" --repo "$REPO")
    [ -n "$TICKET_FROM" ] && args+=(--from "$TICKET_FROM")
    [ -n "$TICKET_TO" ] && args+=(--to "$TICKET_TO")
    [ "$DRY_RUN" -eq 1 ] && args+=(--dry-run)
    bash "$MULTICA_ROOT/scripts/ai-company/sync-backlog-to-issues.sh" "${args[@]}"
  fi
fi

cat <<EOF

== Manual steps ==
  1. CEO machine: cursor-agent login
  2. (Optional) GitHub Secret: SLACK_WEBHOOK_URL
  3. Branch protection on main: require CI checks
  4. Local verify:
       cd $PROJECT_DIR && pnpm install && make check
  5. Dispatch trial:
       bash $MULTICA_ROOT/scripts/agent-delivery/dispatch-cursor-agent-cli.sh <issue#>

Company OS docs: $MULTICA_ROOT/.ai-company/README.md
EOF
