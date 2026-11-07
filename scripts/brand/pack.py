"""Packs the frames scripts/brand/generate.mjs renders into icon.ico and the README GIFs.

Usage: python3 pack.py <render dir> <repo root>. Needs Pillow.
"""
import glob
import sys

from PIL import Image

out, root = sys.argv[1:3]

sizes = [16, 24, 32, 48, 64, 128, 256]
ico = [Image.open(f"{out}/ico-{s}.png").convert("RGBA") for s in sizes]
ico[-1].save(f"{root}/apps/desktop/build/icon.ico", format="ICO", sizes=[(s, s) for s in sizes], append_images=ico[:-1])

for name in ("light", "dark"):
    frames = [Image.open(f).convert("RGB") for f in sorted(glob.glob(f"{out}/gif-{name}/f*.png"))]
    # One palette for every frame, taken from the finished mark, so the strokes never shimmer.
    palette = frames[-1].quantize(colors=64, method=Image.Quantize.MEDIANCUT)
    gif = [f.quantize(palette=palette, dither=Image.Dither.NONE) for f in frames]
    gif[0].save(f"{root}/docs/assets/logo-{name}.gif", save_all=True, append_images=gif[1:], duration=33, optimize=True, disposal=1)
