import Color from "colorjs.io";

export function colorToHex(value: string): string {
  const rgb = new Color(value).to("srgb").toGamut();
  return `#${rgb.coords
    .map((channel) =>
      Math.round(Math.max(0, Math.min(1, channel ?? 0)) * 255)
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")
    .toUpperCase()}`;
}
export function isSrgb(value: string): boolean {
  return new Color(value).inGamut("srgb");
}
export function hexToOklch(value: string): string | null {
  const hex = value.trim().replace(/^#?/, "#");
  if (!/^#(?:[\da-f]{3}|[\da-f]{6})$/i.test(hex)) return null;
  const [l, c, h] = new Color(hex).to("oklch").coords;
  const coords = [
    Math.max(0, Math.min(1, l ?? 0)),
    Math.max(0, c ?? 0),
    Number.isFinite(h) ? ((h! % 360) + 360) % 360 : 0,
  ];
  return `oklch(${coords.map((n) => Number(n.toFixed(6))).join(" ")})`;
}
