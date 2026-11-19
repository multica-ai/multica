import { attachmentIdFromDownloadURL } from "@multica/core/types";

export interface ImageRequest {
  uri: string;
  headers?: Record<string, string>;
}

/**
 * Where to load a body's image from. Attachment links, whichever host they were written with, go
 * to this server's download route with the session token; it answers with the file or a signed
 * redirect. Other absolute URLs load as they are.
 */
export function imageRequest(url: string, apiUrl: string, token: string | null): ImageRequest | null {
  const auth = token ? { Authorization: `Bearer ${token}` } : undefined;
  const id = attachmentIdFromDownloadURL(url);
  if (id) return { uri: `${apiUrl}/api/attachments/${id}/download`, headers: auth };
  if (url.startsWith("/") && !url.startsWith("//")) return { uri: `${apiUrl}${url}`, headers: auth };
  if (!/^https?:\/\//i.test(url)) return null;
  return new URL(url).origin === new URL(apiUrl).origin ? { uri: url, headers: auth } : { uri: url };
}

/** The attachment a link points at, if it is one. */
export function linkedAttachmentId(url: string): string | null {
  return attachmentIdFromDownloadURL(url) ?? null;
}

/**
 * A URL Safari can open for an attachment, from its freshly fetched record: a signed storage URL
 * when the server hands one out, otherwise the short-lived credential-free route.
 */
export function attachmentOpenUrl(record: { download_url: string; attachment_download_url?: string }, apiUrl: string): string | null {
  const absolute = (value: string) => (value.startsWith("/") ? `${apiUrl}${value}` : value);
  const signed = record.download_url && /^https?:\/\//i.test(record.download_url) && attachmentIdFromDownloadURL(record.download_url) === undefined;
  if (signed) return record.download_url;
  if (record.attachment_download_url) return absolute(record.attachment_download_url);
  return null;
}
