import { Alert, Linking } from "react-native";
import { api } from "./api";
import { currentSessionEpoch, sessionEpochChanged } from "./session-epoch";
import { attachmentSource, selectedAttachmentId } from "@/lib/attachment-source";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { i18n } from "@/lib/i18n/singleton";

/** Exchange a stable attachment ID for a new signed URL/proxy capability. */
export async function freshAttachmentUrl(source: string, download = false, signal?: AbortSignal): Promise<string> {
  const epoch = currentSessionEpoch();
  const uri = attachmentSource(source);
  const id = selectedAttachmentId(uri);
  if (!id) return uri;
  const attachment = await api.getAttachmentForDownload(id, { signal });
  if (sessionEpochChanged(epoch)) throw new Error("Server session changed");
  const url = download
    ? attachment.attachment_download_url || attachment.download_url
    : attachment.download_url;
  if (!url) throw new Error("Attachment download URL missing");
  return resolveAttachmentUrl(url)!;
}

export async function openAttachment(source: string): Promise<void> {
  const epoch = currentSessionEpoch();
  try {
    const url = await freshAttachmentUrl(source, true);
    if (!sessionEpochChanged(epoch)) await Linking.openURL(url);
  } catch {
    if (!sessionEpochChanged(epoch)) {
      Alert.alert(i18n.t("settings:server.attachment_failed"));
    }
  }
}
