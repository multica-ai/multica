export function isSafeExternalHttpUrl(url: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }

  return parsed.protocol === "https:" || parsed.protocol === "http:";
}
