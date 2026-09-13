#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/build-operator-distribution.sh"
SOURCE_SKILL="$ROOT_DIR/skills/multica-operator"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_fails() { if "$@" >/dev/null 2>&1; then fail "expected failure: $*"; fi; }

tmp="$(mktemp -d)"
source_link="$SOURCE_SKILL/build-test-symlink"
trap 'rm -rf "$tmp"; rm -f "$source_link"' EXIT

assert_fails bash "$SCRIPT"
assert_fails bash "$SCRIPT" v1.2.3 "$tmp/bad-v"
assert_fails bash "$SCRIPT" 01.2.3 "$tmp/bad-leading-zero"
assert_fails bash "$SCRIPT" 1.2.3-rc.1 "$tmp/bad-prerelease"

mkdir -p "$tmp/nonempty"
touch "$tmp/nonempty/existing"
assert_fails bash "$SCRIPT" 1.2.3 "$tmp/nonempty"

ln -s SKILL.md "$source_link"
assert_fails bash "$SCRIPT" 1.2.3 "$tmp/symlink"
rm -f "$source_link"

bash "$SCRIPT" 1.2.3 "$tmp/output"

marketplace="$tmp/output/marketplace"
plugin="$marketplace/plugins/multica-operator"
staged_skill="$plugin/skills/multica-operator"
release_dir="$tmp/output/release"
archive="$release_dir/multica-operator-1.2.3.tar.gz"
checksum="$archive.sha256"

[[ "$(<"$SOURCE_SKILL/VERSION")" == "dev" ]] || fail "canonical VERSION changed"
[[ "$(<"$staged_skill/VERSION")" == "1.2.3" ]] || fail "staged VERSION is wrong"
[[ -f "$staged_skill/LICENSE" ]] || fail "staged Skill has no LICENSE"
[[ -f "$archive" && -f "$checksum" ]] || fail "release asset pair is incomplete"

for manifest in \
  "$plugin/plugin.json" \
  "$plugin/.codex-plugin/plugin.json" \
  "$plugin/.claude-plugin/plugin.json"; do
  [[ "$(jq -r .name "$manifest")" == "multica-operator" ]] || fail "wrong plugin name: $manifest"
  [[ "$(jq -r .version "$manifest")" == "1.2.3" ]] || fail "wrong plugin version: $manifest"
done

[[ "$(jq -r '.plugins[0].source.path' "$marketplace/.agents/plugins/marketplace.json")" == "./plugins/multica-operator" ]] || fail "wrong Codex source"
[[ "$(jq -r '.plugins[0].version' "$marketplace/.claude-plugin/marketplace.json")" == "1.2.3" ]] || fail "wrong Claude version"
[[ "$(jq -r .version "$marketplace/release.json")" == "1.2.3" ]] || fail "wrong release metadata version"

mkdir -p "$tmp/extract"
tar -xzf "$archive" -C "$tmp/extract"
diff -ru "$staged_skill" "$tmp/extract/multica-operator" >/dev/null || fail "marketplace and archive Skills differ"

(
  cd "$release_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$(basename "$checksum")"
  else
    shasum -a 256 -c "$(basename "$checksum")"
  fi
) >/dev/null || fail "release checksum does not match"

if find "$tmp/output" -type l -print -quit | grep -q .; then
  fail "distribution contains a symbolic link"
fi

workflow="$ROOT_DIR/.github/workflows/release.yml"
for contract in \
  'bash scripts/build-operator-distribution.sh' \
  'gh release view "$TAG_NAME" --json assets' \
  'Incomplete Skill release assets' \
  'remote_version=' \
  'Marketplace already has newer version'; do
  grep -Fq "$contract" "$workflow" || fail "release workflow is missing: $contract"
done
for superseded in \
  'stage-operator-distribution.sh' \
  'package-operator-skill.sh' \
  'gh release upload "$TAG_NAME" /tmp/multica-operator-assets/* --clobber'; do
  ! grep -Fq "$superseded" "$workflow" || fail "release workflow retains: $superseded"
done

printf 'PASS: unified operator distribution build\n'
