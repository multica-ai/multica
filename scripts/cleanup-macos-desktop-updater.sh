#!/usr/bin/env bash
# Remove Multica desktop auto-updater caches and ShipIt backup copies.
#
# electron-updater (Squirrel.Mac) stages ZIP downloads and swaps /Applications/Multica.app
# via ShipIt. Failed signature checks or interrupted installs leave duplicate
# Multica.app* folders that Spotlight indexes.
#
# Safe to run while Multica is quit. Re-launching Multica re-creates updater caches.
set -euo pipefail

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
  shift
fi

remove_path() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return 0
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "would remove: $path"
  else
    echo "removing: $path"
    rm -rf "$path"
  fi
}

echo "Cleaning Multica desktop updater artifacts..."

# electron-updater download cache (ZIP + blockmap + pending metadata)
remove_path "${HOME}/Library/Caches/@multicadesktop-updater"
remove_path "${HOME}/Library/Caches/ai.multica.desktop"
remove_path "${HOME}/Library/Caches/ai.multica.desktop.ShipIt"

# ShipIt leaves timestamped backups beside the live install when swaps fail.
while IFS= read -r backup; do
  remove_path "$backup"
done < <(find /Applications -maxdepth 1 -name 'Multica.app.backup-*' 2>/dev/null || true)

# Stale partial extracts under /private/var/folders (best-effort; paths vary per boot).
while IFS= read -r shipit_dir; do
  remove_path "$shipit_dir"
done < <(find "${TMPDIR:-/tmp}" -maxdepth 3 -name 'ai.multica.desktop.ShipIt' 2>/dev/null || true)

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "Dry run complete — pass no flags to delete the paths above."
else
  echo "Done. Quit and relaunch Multica before checking for updates again."
fi
