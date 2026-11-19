import type { Attachment } from "@multica/core/types";
import { matchAttachmentByURL } from "@multica/core/attachments/image-sequence";
import { attachmentDownloadPath, attachmentIdFromDownloadURL, attachmentIdFromLocalUploadURL } from "@multica/core/types/attachment-url";
import { getServerConfig } from "@/data/server-config-store";
import { resolveAttachmentUrl } from "./attachment-url";

/** Only selected-instance paths (or a known attachment record) use our API. */
export function attachmentSource(uri: string, attachments?: readonly Attachment[]): string {
  const match = matchAttachmentByURL(uri, attachments);
  if (match) return resolveAttachmentUrl(attachmentDownloadPath(match.id))!;
  if (uri.startsWith("mc://file/")) {
    const id = uri.slice("mc://file/".length);
    if (/^[a-zA-Z0-9-]+$/.test(id)) return resolveAttachmentUrl(attachmentDownloadPath(id))!;
  }
  return resolveAttachmentUrl(uri) ?? uri;
}

export function selectedAttachmentId(uri: string): string | undefined {
  const apiUrl = getServerConfig().apiUrl;
  const prefix = `${apiUrl}/`;
  if (!uri.startsWith(prefix)) return undefined;
  const path = `/${uri.slice(prefix.length)}`;
  return attachmentIdFromDownloadURL(path) ?? attachmentIdFromLocalUploadURL(path);
}
