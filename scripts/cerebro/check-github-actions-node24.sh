#!/usr/bin/env bash
set -euo pipefail

# Keep every external JavaScript action on a reviewed, commit-pinned Node.js 24
# build. GitHub currently forces Node.js 20 actions onto Node.js 24, but that
# compatibility path is deprecated and will eventually become a hard failure.
readonly approved_actions='^(actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10|actions/setup-node@249970729cb0ef3589644e2896645e5dc5ba9c38|actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16|actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1|actions/cache@caa296126883cff596d87d8935842f9db880ef25|actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f|actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c|Azure/setup-helm@9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310|docker/build-push-action@bcafcacb16a39f128d818304e6c9c0c18556b85f|docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0|docker/metadata-action@030e881283bb7a6894de51c315a6bfe6a94e05cf|docker/setup-buildx-action@bb05f3f5519dd87d3ba754cc423b652a5edd6d2c|goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94|pnpm/action-setup@0ebf47130e4866e96fce0953f49152a61190b271)$'

status=0
while IFS= read -r action; do
  if [[ "$action" == ./* ]] || [[ "$action" =~ $approved_actions ]]; then
    continue
  fi
  printf 'Unapproved or pre-Node.js 24 GitHub Action: %s\n' "$action" >&2
  status=1
done < <(
  grep -RhoE 'uses:[[:space:]]+[^[:space:]#]+' .github/workflows \
    | awk '{ print $2 }' \
    | sort -u
)

exit "$status"
