#!/usr/bin/env bash
set -euo pipefail

# Build release archives for the Multica CLI across supported desktop/server
# platforms. Archives are written to artifacts/cli/releases/ using the
# update/installer naming convention:
#   multica-cli-<version>-<goos>-<goarch>.<tar.gz|zip>
#
# The release manifest is written to artifacts/cli/manifest.json.

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
SERVER_DIR="${REPO_ROOT}/server"
OUT_DIR="${CLI_ARTIFACTS_DIR:-${REPO_ROOT}/artifacts/cli}"
OBS_ARTIFACTS_DIR="${OBS_ARTIFACTS_DIR:-${REPO_ROOT}/artifacts/obs}"
RELEASES_DIR="${OUT_DIR}/releases"
MANIFEST_FILE="${OUT_DIR}/manifest.json"
DOWNLOAD_BASE_URL="${CLI_DOWNLOAD_BASE_URL:-https://multica.obs.cn-east-3.myhuaweicloud.com/cli/releases}"

DEFAULT_TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

normalize_archive_version() {
  local version="$1"
  printf '%s' "${version#v}"
}

archive_ext() {
  local goos="$1"
  if [[ "$goos" == "windows" ]]; then
    printf 'zip'
  else
    printf 'tar.gz'
  fi
}

binary_name() {
  local goos="$1"
  if [[ "$goos" == "windows" ]]; then
    printf 'multica.exe'
  else
    printf 'multica'
  fi
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    fail "required command not found: sha256sum or shasum"
  fi
}

file_size() {
  local file="$1"
  if stat -c '%s' "$file" >/dev/null 2>&1; then
    stat -c '%s' "$file"
  else
    stat -f '%z' "$file"
  fi
}

validate_download_base_url() {
  local value="$1"
  if [[ "$value" =~ [\"\\[:cntrl:]] ]]; then
    fail "CLI_DOWNLOAD_BASE_URL must not contain quotes, backslashes, or control characters"
  fi
}

download_url_for() {
  local archive_name="$1"
  if [[ -z "$DOWNLOAD_BASE_URL" ]]; then
    printf 'releases/%s' "$archive_name"
  else
    printf '%s/%s' "${DOWNLOAD_BASE_URL%/}" "$archive_name"
  fi
}

validate_version() {
  local version="$1"
  if [[ ! "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
    fail "invalid CLI version: ${version}"
  fi
}

resolve_version() {
  if [[ -n "${CLI_VERSION:-}" ]]; then
    VERSION_SOURCE="CLI_VERSION"
  else
    VERSION_SOURCE="git-derived CLI version"
  fi

  VERSION_RAW="$(bash "${SCRIPT_DIR}/derive-cli-version.sh")"
  if [[ -n "$VERSION_RAW" ]]; then
    return
  fi

  VERSION_RAW="dev"
  VERSION_SOURCE="fallback"
}

target_supported() {
  local target="$1"
  case "$target" in
    darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64|windows/arm64)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

need_cmd go
need_cmd tar
need_cmd zip
need_cmd node
validate_download_base_url "$DOWNLOAD_BASE_URL"

VERSION_RAW=""
VERSION_SOURCE=""
resolve_version
[[ -n "$VERSION_RAW" ]] || fail "CLI version is empty"
validate_version "$VERSION_RAW"
ARCHIVE_VERSION="$(normalize_archive_version "$VERSION_RAW")"

COMMIT="${COMMIT:-$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
DATE="${DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
LDFLAGS="-s -w -X main.version=${VERSION_RAW} -X main.commit=${COMMIT} -X main.date=${DATE}"

TARGETS=("${DEFAULT_TARGETS[@]}")
if [[ -n "${MULTICA_CLI_TARGETS:-}" ]]; then
  # Space-separated list, e.g. MULTICA_CLI_TARGETS="darwin/arm64 linux/amd64".
  read -r -a TARGETS <<< "${MULTICA_CLI_TARGETS}"
fi

mkdir -p "$RELEASES_DIR"
TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

CHECKSUMS_FILE="${RELEASES_DIR}/checksums.txt"
: > "$CHECKSUMS_FILE"
ASSET_ROWS=()

printf 'Building Multica CLI artifacts\n'
printf '  version:   %s\n' "$VERSION_RAW"
printf '  source:    %s\n' "$VERSION_SOURCE"
printf '  commit:    %s\n' "$COMMIT"
printf '  date:      %s\n' "$DATE"
printf '  output:    %s\n' "$OUT_DIR"
printf '  obs tool:  %s\n' "$OBS_ARTIFACTS_DIR"
printf '  releases:  %s\n' "$RELEASES_DIR"
printf '  manifest:  %s\n' "$MANIFEST_FILE"
printf '\n'

bash "${SCRIPT_DIR}/prepare-cli-runtime-assets.sh"

for target in "${TARGETS[@]}"; do
  target="$(trim "$target")"
  [[ -n "$target" ]] || continue
  target_supported "$target" || fail "unsupported target: ${target}"

  goos="${target%/*}"
  goarch="${target#*/}"
  bin_name="$(binary_name "$goos")"
  ext="$(archive_ext "$goos")"
  archive_name="multica-cli-${ARCHIVE_VERSION}-${goos}-${goarch}.${ext}"
  archive_path="${RELEASES_DIR}/${archive_name}"
  stage_dir="${TMP_DIR}/${goos}-${goarch}"

  mkdir -p "$stage_dir"
  printf '==> %s/%s\n' "$goos" "$goarch"
  rm -f "$archive_path"

  (
    cd "$SERVER_DIR"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "${stage_dir}/${bin_name}" \
        ./cmd/multica
  )

  cp -R "${REPO_ROOT}/.dist/cli-runtime/${goos}-${goarch}/." "$stage_dir/"

  if [[ "$goos" == "windows" ]]; then
    (cd "$stage_dir" && zip -q -9 -r "$archive_path" .)
  else
    chmod 0755 "${stage_dir}/${bin_name}"
    tar -C "$stage_dir" -czf "$archive_path" .
  fi

  checksum="$(sha256_file "$archive_path")"
  size="$(file_size "$archive_path")"
  download_url="$(download_url_for "$archive_name")"
  printf '%s  %s\n' "$checksum" "$archive_name" >> "$CHECKSUMS_FILE"
  ASSET_ROWS+=("${goos}|${goarch}|${archive_name}|${download_url}|${checksum}|${size}")
  printf '    wrote %s\n' "$archive_path"
done

{
  printf '{\n'
  printf '  "version": "%s",\n' "$VERSION_RAW"
  printf '  "commit": "%s",\n' "$COMMIT"
  printf '  "date": "%s",\n' "$DATE"
  printf '  "assets": [\n'
  for i in "${!ASSET_ROWS[@]}"; do
    IFS='|' read -r goos goarch archive_name download_url checksum size <<< "${ASSET_ROWS[$i]}"
    printf '    {\n'
    printf '      "os": "%s",\n' "$goos"
    printf '      "arch": "%s",\n' "$goarch"
    printf '      "filename": "%s",\n' "$archive_name"
    printf '      "download_url": "%s",\n' "$download_url"
    printf '      "checksum": "%s",\n' "$checksum"
    printf '      "size": %s\n' "$size"
    if [[ "$i" -lt $((${#ASSET_ROWS[@]} - 1)) ]]; then
      printf '    },\n'
    else
      printf '    }\n'
    fi
  done
  printf '  ]\n'
  printf '}\n'
} > "$MANIFEST_FILE"

printf '\nBuilding OBS release publisher\n'
mkdir -p "$OBS_ARTIFACTS_DIR"
(
  cd "$SERVER_DIR"
  go build \
    -trimpath \
    -ldflags "$LDFLAGS" \
    -o "${OBS_ARTIFACTS_DIR}/multica-obs-release" \
    ./cmd/obs-release
)
printf 'Done. OBS release publisher: %s\n' "${OBS_ARTIFACTS_DIR}/multica-obs-release"

printf '\nDone. Manifest: %s\n' "$MANIFEST_FILE"
printf 'Done. Checksums: %s\n' "$CHECKSUMS_FILE"
