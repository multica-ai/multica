#!/usr/bin/env bash
# Minimal competitor research fallback when cursor-agent research phase fails.
# shellcheck shell=bash

site_factory_write_research_fallback() {
  local delivery_dir="$1"
  local slug="$2"
  local idea="$3"
  mkdir -p "$delivery_dir"
  cat >"$delivery_dir/research.md" <<EOF
# Research — $slug (pipeline fallback)

> Auto-generated because the research agent did not produce \`research.md\`. CEO should review before treating competitors as verified.

## Problem

Build a minimal Cloudflare Pages site for: **$idea**.

## Target users

- Developers and curious visitors evaluating a static demo
- English-first; mobile-friendly

## Competitors (desk research — verify URLs)

| Name | URL | Strengths | Weaknesses |
|------|-----|-----------|------------|
| Cloudflare Pages docs demo | https://developers.cloudflare.com/pages/ | Official, fast edge | Not a product |
| Vercel hello templates | https://vercel.com/templates | Polished DX | **Out of stack** (we use CF only) |
| Netlify drop | https://app.netlify.com/drop | Zero-config static | **Out of stack** |

## Differentiation

- **Cloudflare-only** stack (Pages + Wrangler), no Vercel/Netlify
- Single-purpose hello/demo page with clear CTA and privacy/terms

## SEO keywords

- cloudflare pages demo
- static site hello world
- edge hosted landing page

## Recommended MVP (≤5 items)

1. \`/\` hero + "Hello Cloudflare" message
2. \`/privacy\` and \`/terms\` static pages
3. SEO title/description on \`index.html\`
4. \`make check\` + Pages build green in CI
5. No auth, no backend, no database

## Risks

- None for static demo; ensure no secrets in repo
EOF
}

site_factory_write_mvp_fallback() {
  local delivery_dir="$1"
  local slug="$2"
  local idea="$3"
  local example="$MULTICA_ROOT/.ai-company/examples/cloudflare-site"
  mkdir -p "$delivery_dir"
  cp "$example/brief.md" "$example/accept_cases.md" "$example/backlog.md" \
    "$example/competitor_inventory.md" "$example/wont_do.md" \
    "$delivery_dir/" 2>/dev/null || true
  # Ensure gate files exist even if example copy partially failed
  if [ ! -f "$delivery_dir/competitor_inventory.md" ]; then
    cp "$MULTICA_ROOT/.ai-company/templates/competitor_inventory.md" "$delivery_dir/competitor_inventory.md"
  fi
  if [ ! -f "$delivery_dir/wont_do.md" ]; then
    cp "$MULTICA_ROOT/.ai-company/templates/wont_do.md" "$delivery_dir/wont_do.md"
  fi
  sed -i '' "s/cloudflare-site/$slug/g" "$delivery_dir/brief.md" 2>/dev/null || \
    sed -i "s/cloudflare-site/$slug/g" "$delivery_dir/brief.md" 2>/dev/null || true
  sed -i '' "s/<project-slug>/$slug/g" "$delivery_dir/competitor_inventory.md" "$delivery_dir/wont_do.md" 2>/dev/null || \
    sed -i "s/<project-slug>/$slug/g" "$delivery_dir/competitor_inventory.md" "$delivery_dir/wont_do.md" 2>/dev/null || true
  python3 - "$delivery_dir/brief.md" "$idea" <<'PY'
from pathlib import Path
import re
import sys

path, idea = Path(sys.argv[1]), sys.argv[2]
text = path.read_text(encoding="utf-8")
text = re.sub(
    r"1\. 海外用户可用的 \*\*单页工具或营销站\*\*（示例：JSON 格式化器）— SEO 友好、加载快。",
    f"1. **{idea}** — minimal Cloudflare Pages demo (SEO-friendly, fast).",
    text,
    count=1,
)
text = re.sub(
    r"- `/` 工具 UI 或落地页 \+ 简短 SEO 文案",
    "- `/` hero showing the intake idea (minimal demo) + short SEO copy",
    text,
    count=1,
)
path.write_text(text, encoding="utf-8")
PY
}
