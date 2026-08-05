#!/usr/bin/env node

import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourcePath = resolve(repoRoot, "apps/desktop/resources/icon.png");
const outputDirectory = resolve(repoRoot, "apps/web/public/icons");
const maskableSize = 512;
const maskableInsetSize = Math.round(maskableSize * 0.8);
const maskablePadding = Math.floor((maskableSize - maskableInsetSize) / 2);

const pngOptions = {
  adaptiveFiltering: false,
  compressionLevel: 9,
  palette: false,
};

await mkdir(outputDirectory, { recursive: true });

await Promise.all([
  writeIcon("icon-192.png", 192),
  writeIcon("icon-512.png", 512),
  writeMaskableIcon(),
  writeIcon("apple-touch-icon.png", 180),
]);

function writeIcon(filename, size) {
  return sharp(sourcePath)
    .resize(size, size, { fit: "fill", kernel: sharp.kernel.lanczos3 })
    .png(pngOptions)
    .toFile(resolve(outputDirectory, filename));
}

function writeMaskableIcon() {
  return sharp(sourcePath)
    .resize(maskableInsetSize, maskableInsetSize, {
      fit: "fill",
      kernel: sharp.kernel.lanczos3,
    })
    .flatten({ background: "#ffffff" })
    .extend({
      top: maskablePadding,
      bottom: maskableSize - maskableInsetSize - maskablePadding,
      left: maskablePadding,
      right: maskableSize - maskableInsetSize - maskablePadding,
      background: "#ffffff",
    })
    .png(pngOptions)
    .toFile(resolve(outputDirectory, "icon-maskable-512.png"));
}
