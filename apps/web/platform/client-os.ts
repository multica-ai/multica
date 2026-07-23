export type CoarseClientOS =
  | "macos"
  | "windows"
  | "linux"
  | "ios"
  | "android"
  | "chromeos"
  | "unknown";

/** Maps browser hints to a coarse OS bucket without retaining the raw user agent. */
export function detectWebOS(
  nav: Pick<Navigator, "platform" | "userAgent"> | undefined =
    typeof navigator === "undefined" ? undefined : navigator,
): CoarseClientOS {
  if (!nav) return "unknown";
  const hint = `${nav.platform} ${nav.userAgent}`.toLowerCase();
  if (/iphone|ipad|ipod/.test(hint)) return "ios";
  if (hint.includes("android")) return "android";
  if (hint.includes("cros")) return "chromeos";
  if (/mac|darwin/.test(hint)) return "macos";
  if (/win/.test(hint)) return "windows";
  if (/linux|x11/.test(hint)) return "linux";
  return "unknown";
}
