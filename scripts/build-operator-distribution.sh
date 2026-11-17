#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s X.Y.Z <empty-output-dir>\n' "$(basename "$0")" >&2
  exit 2
}

[[ $# -eq 2 ]] || usage
VERSION="$1"
OUTPUT_DIR="$2"
[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || usage
command -v jq >/dev/null 2>&1 || { printf 'jq is required\n' >&2; exit 1; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DIR="$ROOT_DIR/skills/multica-operator"
[[ -d "$SOURCE_DIR" ]] || { printf 'Missing canonical Skill: %s\n' "$SOURCE_DIR" >&2; exit 1; }

if find "$SOURCE_DIR" -mindepth 1 \( -type l -o \( ! -type d -a ! -type f \) \) -print -quit | grep -q .; then
  printf 'Canonical Skill must contain only regular files and directories\n' >&2
  exit 1
fi
if [[ -e "$OUTPUT_DIR" && -n "$(find "$OUTPUT_DIR" -mindepth 1 -print -quit 2>/dev/null)" ]]; then
  printf 'Output directory must be empty: %s\n' "$OUTPUT_DIR" >&2
  exit 1
fi

STAGING_PARENT="$(mktemp -d "${TMPDIR:-/tmp}/multica-operator-build.XXXXXX")"
trap 'rm -rf "$STAGING_PARENT"' EXIT
STAGED_SKILL="$STAGING_PARENT/multica-operator"
mkdir -p "$STAGED_SKILL" "$OUTPUT_DIR/marketplace" "$OUTPUT_DIR/release"
cp -R "$SOURCE_DIR/." "$STAGED_SKILL/"
cp "$ROOT_DIR/LICENSE" "$STAGED_SKILL/LICENSE"
printf '%s\n' "$VERSION" >"$STAGED_SKILL/VERSION"

MARKETPLACE_DIR="$OUTPUT_DIR/marketplace"
PLUGIN_DIR="$MARKETPLACE_DIR/plugins/multica-operator"
mkdir -p "$PLUGIN_DIR/.codex-plugin" "$PLUGIN_DIR/.claude-plugin" \
  "$PLUGIN_DIR/skills" "$MARKETPLACE_DIR/.agents/plugins" "$MARKETPLACE_DIR/.claude-plugin"
cp -R "$STAGED_SKILL" "$PLUGIN_DIR/skills/multica-operator"
cp "$ROOT_DIR/LICENSE" "$MARKETPLACE_DIR/LICENSE"
cp "$ROOT_DIR/LICENSE" "$PLUGIN_DIR/LICENSE"

common_manifest() {
  jq -n --arg version "$VERSION" '{
    name: "multica-operator",
    version: $version,
    description: "Connect to Multica and operate workspace resources and workflows.",
    author: {name: "Multica, Inc.", url: "https://github.com/multica-ai"},
    homepage: "https://multica.ai",
    repository: "https://github.com/multica-ai/multica",
    license: "Multica License",
    keywords: ["multica", "workspace", "issues", "agents", "skills", "autopilot"]
  }'
}

common_manifest >"$PLUGIN_DIR/plugin.json"
common_manifest | jq '. + {
  skills: "./skills/",
  interface: {
    displayName: "Multica Operator",
    shortDescription: "Operate Multica workspaces and workflows",
    longDescription: "Connect through the community CLI, inspect resources, and execute confirmed Multica workflows.",
    developerName: "Multica, Inc.",
    category: "Productivity",
    capabilities: ["Interactive", "Read", "Write"],
    websiteURL: "https://multica.ai",
    defaultPrompt: ["Connect me to Multica.", "Plan a Multica workflow for this goal."]
  }
}' >"$PLUGIN_DIR/.codex-plugin/plugin.json"
common_manifest >"$PLUGIN_DIR/.claude-plugin/plugin.json"

jq -n '{
  name: "multica",
  interface: {displayName: "Multica"},
  plugins: [{
    name: "multica-operator",
    source: {source: "local", path: "./plugins/multica-operator"},
    policy: {installation: "AVAILABLE", authentication: "ON_USE"},
    category: "Productivity"
  }]
}' >"$MARKETPLACE_DIR/.agents/plugins/marketplace.json"

jq -n --arg version "$VERSION" '{
  name: "multica",
  owner: {name: "Multica, Inc.", url: "https://github.com/multica-ai"},
  metadata: {description: "Official Multica agent plugins"},
  plugins: [{
    name: "multica-operator",
    source: "./plugins/multica-operator",
    description: "Operate Multica workspaces and workflows",
    version: $version
  }]
}' >"$MARKETPLACE_DIR/.claude-plugin/marketplace.json"

jq -n --arg version "$VERSION" --arg url "https://github.com/multica-ai/multica/releases/tag/v$VERSION" '{
  version: $version,
  release_url: $url
}' >"$MARKETPLACE_DIR/release.json"

ARCHIVE_NAME="multica-operator-$VERSION.tar.gz"
ARCHIVE_PATH="$OUTPUT_DIR/release/$ARCHIVE_NAME"
tar -czf "$ARCHIVE_PATH" -C "$STAGING_PARENT" multica-operator
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="$(sha256sum "$ARCHIVE_PATH" | awk '{print $1}')"
else
  SHA256="$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')"
fi
printf '%s  %s\n' "$SHA256" "$ARCHIVE_NAME" >"$ARCHIVE_PATH.sha256"
