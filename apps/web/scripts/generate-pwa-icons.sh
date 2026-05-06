#!/usr/bin/env bash
# Regenerate PWA icons from public/favicon.svg.
#
# Usage: bash apps/web/scripts/generate-pwa-icons.sh
# Requires: rsvg-convert (librsvg) and magick (ImageMagick).
#   macOS:   brew install librsvg imagemagick
#   Linux:   apt-get install librsvg2-bin imagemagick
#
# Output is committed to public/icons/. Re-run only if the source SVG changes.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
public="$here/../public"
src="$public/favicon.svg"
out="$public/icons"
mkdir -p "$out"

# "any" purpose icons render the SVG at full size on a white background —
# favicon.svg already has a rounded rect built in, so this matches the
# brand exactly.
rsvg-convert -w 192 -h 192 -b "#ffffff" "$src" -o "$out/icon-192.png"
rsvg-convert -w 512 -h 512 -b "#ffffff" "$src" -o "$out/icon-512.png"
rsvg-convert -w 180 -h 180 -b "#ffffff" "$src" -o "$out/apple-touch-icon.png"

# "maskable" needs a safe zone so Android adaptive icon masks never crop the
# glyph. Render the logo at 80% of the canvas, then composite onto a full
# 512x512 white square.
tmp="$(mktemp -t pwa-inner.XXXXXX.png)"
trap 'rm -f "$tmp"' EXIT
rsvg-convert -w 410 -h 410 "$src" -o "$tmp"
magick -size 512x512 xc:white "$tmp" -gravity center -composite "$out/icon-maskable-512.png"

echo "Wrote: $(ls "$out")"
