# Brand assets

Every logo file in the repo is generated from `packages/ui/lib/multica-brand.ts`.
After editing that file, regenerate and commit what changed:

```sh
pnpm generate:brand
```

On an unchanged `multica-brand.ts` the command normally leaves the working
tree clean, byte for byte. GPU rasterisation occasionally moves a single pixel
by one step in one channel. A bitmap showing up as modified when the geometry
didn't change is that noise. Discard it with `git checkout -- <file>`.

## Prerequisites

Run it on macOS. The committed bitmaps were rendered there. Other platforms
are untested, and they skip `icon.icns` because `iconutil` is macOS-only.

- Node 22.18 or later, for `.ts` type stripping, with `pnpm install` done.
- Playwright's Chromium: `pnpm exec playwright install chromium`. The script
  uses the full Chrome for Testing build (`channel: "chromium"`), which
  rasterises on the GPU. The default headless shell, or `--disable-gpu`,
  rasterises in software and changes every bitmap.
- Python 3 with Pillow (`pip install pillow`), for `icon.ico` and the GIFs.
- `oxipng` (`brew install oxipng`).
- `iconutil` for `icon.icns`, which ships with macOS.

## What it writes

`sync.mjs` writes the text copies. It needs no dependencies, so CI runs it
with `--check` and fails when a copy drifts from `multica-brand.ts`.

- `apps/web/public/favicon.svg`, `apps/docs/app/icon.svg`: favicon, with a dark-mode variant
- `apps/web/public/icons/icon.svg`: full-bleed source for the home-screen icons
- `docs/assets/logo-{light,dark}.svg`: the README wordmark
- `apps/mobile/components/brand/multica-logo.tsx`: the block between the `GENERATED` markers. Mobile does not depend on `@multica/ui`.
- `server/cmd/multica/cmd_auth.go`: the `.brand` SVG on the `multica login` callback page

`generate.mjs` runs `sync.mjs`, then renders the bitmaps in Chromium:

| File | Size | Shape |
| --- | --- | --- |
| `apps/desktop/build/icons/*.png`, `build/icon.png` | 16–1024 | tile with transparent margin |
| `apps/desktop/build/icon.ico` | 16–256 | same tiles, packed by Pillow |
| `apps/desktop/resources/icon.png`, `build/icon.icns` | 16–1024 | Apple's 1024 grid (body 100–924) with a soft shadow |
| `apps/web/public/icons/icon-{192,512}.png` | 192, 512 | PWA tile with transparent margin |
| `apps/web/public/icons/apple-touch-icon.png`, `icon-maskable-512.png` | 180, 512 | full-bleed, opaque |
| `apps/mobile/assets/icon.png` | 1024 | full-bleed, opaque |
| `apps/web/app/opengraph-image.png` | 1200×630 | OG card, Inter from `@fontsource-variable/inter` |
| `docs/assets/logo-{light,dark}.gif` | 720×256 | README write-on, 30 fps, 64 colours |

At 24px and below, the star grows and thickens (`iconStar`). The margins in
`FLAT` follow the existing desktop sizes. Every PNG then goes through
`oxipng -o max --strip safe`.

## Recording the GIF

The write-on timings mirror `MulticaWordmark` (`multica-wordmark.tsx`) and its
keyframes in `packages/ui/styles/base.css`. Change all three together.

Frames are captured by pausing every animation and seeking it to each frame's
time. Do not use CDP virtual time instead: transform animations, like the star
spinning in, do not advance under it, so the star never appears.

After each seek, wait two animation frames before taking the screenshot. The
star's transform reaches the compositor late. Captured sooner, a few frames of
the spin keep pixels from the previous frame, and the GIF differs from run to
run.
