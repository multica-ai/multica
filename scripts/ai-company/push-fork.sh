#!/usr/bin/env bash
# Push current branch to your fork (chenzh/multica). Do not push to multica-ai/multica.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BRANCH="${1:-$(git -C "$MULTICA_ROOT" branch --show-current)}"
REMOTE="${PUSH_REMOTE:-fork}"

cd "$MULTICA_ROOT"

if ! git remote get-url "$REMOTE" &>/dev/null; then
  echo "error: remote '$REMOTE' not found" >&2
  echo "  git remote add fork https://github.com/chenzh/multica.git" >&2
  exit 1
fi

echo ">> git push $REMOTE $BRANCH"
GIT_CREDENTIAL_ARGS=()
if command -v gh >/dev/null 2>&1 && gh auth status -h github.com >/dev/null 2>&1; then
  GIT_CREDENTIAL_ARGS+=(-c "credential.helper=!gh auth git-credential")
fi
git "${GIT_CREDENTIAL_ARGS[@]}" -c http.version=HTTP/1.1 push -u "$REMOTE" "$BRANCH"
