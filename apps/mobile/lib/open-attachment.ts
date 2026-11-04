/**
 * Open an attachment outside the app (Safari / QuickLook / share sheet).
 *
 * Why this exists: list and upload responses hand out the auth-gated
 * `/api/attachments/{id}/download` path, and `Linking.openURL` starts a
 * request that carries neither the Bearer token nor a session cookie — the
 * server answers 401 `{"error":"missing authorization"}` and the user sees
 * an error page instead of their file.
 *
 * The fix mirrors web's `useDownloadAttachment` (packages/views/editor/
 * use-download-attachment.ts): re-fetch the record through the
 * authenticated metadata endpoint, which re-signs `download_url` on every
 * call — a CloudFront / S3 presigned URL, or in proxy mode a short-lived
 * single-attachment capability that authenticates itself in the query
 * string. Then hand THAT to the system opener.
 *
 * URLs that carry no attachment id (public CDN links, external links,
 * capability URLs the server already made credential-free) open unchanged.
 */
import { Alert, Linking } from "react-native";
import { attachmentIdFromDownloadURL, type Attachment } from "@multica/core/types";
import { api } from "@/data/api";
import { i18n } from "@/lib/i18n/singleton";
import { resolveAttachmentUrl } from "./attachment-url";

export async function openAttachment(
  rawUrl: string | null | undefined,
): Promise<void> {
  if (!rawUrl) return;

  const attachmentId = attachmentIdFromDownloadURL(rawUrl);
  let target = resolveAttachmentUrl(rawUrl);

  if (attachmentId) {
    // The stored URL is the auth-gated path, so it cannot serve as a
    // fallback: Safari would launch and show 401 `missing authorization`.
    // Without a fresh signed URL, report the failure in-app instead.
    let fresh: Attachment | undefined;
    try {
      fresh = await api.getAttachmentForDownload(attachmentId);
    } catch {
      // Fall through to the alert below.
    }
    // `id: ""` is the parse fallback.
    target = fresh?.id ? resolveAttachmentUrl(fresh.download_url) : null;
    if (!target) {
      Alert.alert(i18n.t("common:attachments.open_failed"));
      return;
    }
  }

  if (!target) return;
  try {
    await Linking.openURL(target);
  } catch {
    Alert.alert(i18n.t("common:attachments.open_failed"));
  }
}
