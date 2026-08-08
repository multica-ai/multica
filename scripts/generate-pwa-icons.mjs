#!/usr/bin/env node

import { mkdir, readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const glyphSourcePath = resolve(repoRoot, "apps/web/public/favicon.svg");
const outputDirectory = resolve(repoRoot, "apps/web/public/icons");

// Full bleed, no rounding, no shadow, no alpha: every launcher applies its own
// mask, and baking a second shape inside that one only ever double-rounds.
const background = "#05070b";
const glyphColor = "#d1d5db";
const glyphScale = 0.62;

const pngOptions = {
  adaptiveFiltering: false,
  compressionLevel: 9,
  palette: false,
};

const glyphPoints = await readGlyphPoints();

await mkdir(outputDirectory, { recursive: true });

await Promise.all([
  writeIcon("icon-192.png", 192),
  writeIcon("icon-512.png", 512),
  writeIcon("icon-maskable-512.png", 512),
  writeIcon("apple-touch-icon.png", 180),
]);

async function readGlyphPoints() {
  const svg = await readFile(glyphSourcePath, "utf8");
  const match = svg.match(/<polygon[^>]*\spoints="([^"]+)"/);

  if (!match) {
    throw new Error(`No <polygon points="..."> found in ${glyphSourcePath}`);
  }

  return match[1].replace(/\s+/g, " ").trim();
}

function writeIcon(filename, size) {
  return sharp(Buffer.from(renderSvg(size)))
    .flatten({ background })
    .png(pngOptions)
    .toFile(resolve(outputDirectory, filename));
}

function renderSvg(size) {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 100 100">
  <rect width="100" height="100" fill="${background}"/>
  <g transform="translate(50 50) scale(${glyphScale}) translate(-50 -50)">
    <polygon fill="${glyphColor}" points="${glyphPoints}"/>
  </g>
</svg>`;
}
