/** Classify the URL host without DNS; IP targets belong to the source device. */
export function previewServiceHost(value: string): string | null {
  try {
    const url = new URL(value);
    if (!["http:", "https:", "ws:", "wss:"].includes(url.protocol) || url.username || url.password) return null;
    const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, "").replace(/\.$/, "");
    if (host === "localhost" || host.endsWith(".localhost")) return "localhost";
    // WHATWG URL parsing has already canonicalized IPv4 and validated IPv6.
    return host.includes(":") || /^\d+\.\d+\.\d+\.\d+$/.test(host) ? host : null;
  } catch { return null; }
}

export function isServicePreviewUrl(value: string): boolean {
  return previewServiceHost(value) !== null;
}
