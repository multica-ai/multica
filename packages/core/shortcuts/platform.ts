export type ShortcutPlatform = "macos" | "windows" | "linux" | "unknown";

let configuredPlatform: ShortcutPlatform | null = null;

function normalizePlatform(value: string): ShortcutPlatform {
  const platform = value.toLowerCase();
  if (platform.includes("mac") || platform.includes("darwin")) return "macos";
  if (platform.includes("win")) return "windows";
  if (platform.includes("linux") || platform.includes("x11")) return "linux";
  return "unknown";
}

/** Browser fallback. Desktop injects its authoritative OS through CoreProvider. */
export function detectShortcutPlatform(): ShortcutPlatform {
  if (typeof navigator === "undefined") return "unknown";
  const nav = navigator as Navigator & {
    userAgentData?: { platform?: string };
  };
  // Some privacy-hardened browsers expose `platform` as an empty string.
  // Nullish coalescing would stop there and discard a still-useful userAgent,
  // causing Command to be interpreted as Windows/Super on a Mac.
  const signals = [nav.userAgentData?.platform, nav.platform, nav.userAgent];
  for (const signal of signals) {
    if (!signal?.trim()) continue;
    const platform = normalizePlatform(signal);
    if (platform !== "unknown") return platform;
  }
  return "unknown";
}

export function configureShortcutPlatform(
  platform: ShortcutPlatform | null | undefined,
): void {
  configuredPlatform = platform ?? null;
}

export function getShortcutPlatform(): ShortcutPlatform {
  return configuredPlatform ?? detectShortcutPlatform();
}
