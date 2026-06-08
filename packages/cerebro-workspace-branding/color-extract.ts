// TECH-3002: color utilities for workspace accent color — auto-extraction +
// conversion between hex, hue, and the oklch sidebar CSS variable format.

/** Convert a 6-digit hex color to its HSL hue angle (0–360). */
export function hexToHue(hex: string): number {
  const r = parseInt(hex.slice(1, 3), 16) / 255;
  const g = parseInt(hex.slice(3, 5), 16) / 255;
  const b = parseInt(hex.slice(5, 7), 16) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  if (d === 0) return 0;
  let h = 0;
  if (max === r) h = ((g - b) / d) % 6;
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return ((h * 60) + 360) % 360;
}

/** Hue angle → [dark oklch, light oklch] pair used for --sidebar CSS variable. */
export function hueToOklchPair(hue: number): [string, string] {
  const h = hue.toFixed(1);
  return [`oklch(0.20 0.030 ${h})`, `oklch(0.95 0.015 ${h})`];
}

/** Hue angle → a representative hex color suitable for storage + color picker display. */
export function hueToHex(hue: number): string {
  const s = 0.65;
  const l = 0.55;
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = l - c / 2;
  let r = 0, g = 0, b = 0;
  if (hue < 60) { r = c; g = x; }
  else if (hue < 120) { r = x; g = c; }
  else if (hue < 180) { g = c; b = x; }
  else if (hue < 240) { g = x; b = c; }
  else if (hue < 300) { r = x; b = c; }
  else { r = c; b = x; }
  const toHex = (v: number) => Math.round((v + m) * 255).toString(16).padStart(2, "0");
  return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
}

/**
 * Load an image URL into a 64×64 canvas, find the most-saturated pixel, and
 * return its HSL hue. Returns null on CORS failure, greyscale images, or if
 * no sufficiently saturated pixel is found (saturation threshold: 0.2).
 */
export async function extractHueFromUrl(url: string): Promise<number | null> {
  if (typeof document === "undefined") return null;
  return new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = () => {
      try {
        const size = 64;
        const canvas = document.createElement("canvas");
        canvas.width = size;
        canvas.height = size;
        const ctx = canvas.getContext("2d");
        if (!ctx) { resolve(null); return; }
        ctx.drawImage(img, 0, 0, size, size);
        const { data } = ctx.getImageData(0, 0, size, size);
        let bestSat = 0;
        let bestHue = 0;
        for (let i = 0; i < data.length; i += 4) {
          const alpha = (data[i + 3] ?? 0) / 255;
          if (alpha < 0.5) continue;
          const r = (data[i] ?? 0) / 255;
          const g = (data[i + 1] ?? 0) / 255;
          const b = (data[i + 2] ?? 0) / 255;
          const max = Math.max(r, g, b);
          const min = Math.min(r, g, b);
          const lightness = (max + min) / 2;
          if (lightness < 0.1 || lightness > 0.9) continue;
          const d = max - min;
          if (d === 0) continue;
          const sat = d / (1 - Math.abs(2 * lightness - 1));
          if (sat > bestSat) {
            bestSat = sat;
            let h = 0;
            if (max === r) h = ((g - b) / d) % 6;
            else if (max === g) h = (b - r) / d + 2;
            else h = (r - g) / d + 4;
            bestHue = ((h * 60) + 360) % 360;
          }
        }
        resolve(bestSat > 0.2 ? bestHue : null);
      } catch {
        resolve(null);
      }
    };
    img.onerror = () => resolve(null);
    img.src = url;
  });
}
