#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_mod="$repo_root/server/go.mod"

for dockerfile_name in Dockerfile Dockerfile.runtime; do
  dockerfile="$repo_root/$dockerfile_name"
  download_line="$(grep -nF 'RUN cd server && go mod download' "$dockerfile" | cut -d: -f1)"
  if [[ -z "$download_line" ]]; then
    echo "$dockerfile_name must run go mod download" >&2
    exit 1
  fi

  while read -r local_target; do
    module_path="${local_target#../}"
    copy_line="$(grep -nE "^COPY ${module_path}/? \\./${module_path}/?$" "$dockerfile" | cut -d: -f1 || true)"
    if [[ -z "$copy_line" ]]; then
      echo "$dockerfile_name must copy local Go module $module_path before go mod download" >&2
      exit 1
    fi
    if (( copy_line >= download_line )); then
      echo "$dockerfile_name copies $module_path after go mod download" >&2
      exit 1
    fi
  done < <(awk '$1 == "replace" && $3 == "=>" && $4 ~ /^\.\.\// { print $4 }' "$go_mod")
done

echo "Dockerfiles copy every local Go module before go mod download"
