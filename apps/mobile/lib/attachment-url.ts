/** Resolve instance-relative URLs without losing the configured proxy prefix. */
import { getServerConfig } from "@/data/server-config-store";
import { joinServerPath } from "./server-url";

export function resolveAttachmentUrlWithBase(
  rawUrl: string | null | undefined,
  baseUrl: string,
): string | null {
  if (!rawUrl) return null;
  if (!rawUrl.startsWith("/")) return rawUrl;
  // A protocol-relative URL is external, never an API path.
  if (rawUrl.startsWith("//")) return `https:${rawUrl}`;
  // Unlike client-generated routes, server-generated paths may already
  // include the deployment's reverse-proxy prefix.
  if (baseUrl) {
    const base = new URL(baseUrl);
    const prefix = base.pathname.replace(/\/+$/, "");
    if (prefix && (rawUrl === prefix || rawUrl.startsWith(`${prefix}/`))) {
      return `${base.origin}${rawUrl}`;
    }
  }
  return joinServerPath(baseUrl, rawUrl);
}

export function resolveAttachmentUrl(rawUrl: string | null | undefined): string | null {
  return resolveAttachmentUrlWithBase(rawUrl, getServerConfig().apiUrl);
}
