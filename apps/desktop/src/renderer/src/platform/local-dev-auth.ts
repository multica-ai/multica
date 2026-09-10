export function localDevTokenForApi(
  apiUrl: string,
  token: string | undefined,
): string | null {
  const candidate = token?.trim();
  if (!candidate) return null;

  try {
    const url = new URL(apiUrl);
    const loopback =
      url.hostname === "localhost" ||
      url.hostname === "127.0.0.1" ||
      url.hostname === "[::1]";
    return url.protocol === "http:" && loopback ? candidate : null;
  } catch {
    return null;
  }
}
