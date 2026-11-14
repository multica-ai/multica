import { Linking } from "react-native";
import { api } from "@multica/core/api";
import { attachmentOpenUrl, imageRequest, linkedAttachmentId } from "../../platform/attachments";
import { endpoints } from "../../platform/env";
import type { MarkdownHost } from "../markdown/host";

async function openAttachment(id: string) {
  const record = await api.getAttachmentForDownload(id);
  const url = attachmentOpenUrl(record, endpoints.apiUrl);
  if (url) await Linking.openURL(url);
}

/** Images load with the session; attachment links open through a freshly signed URL. */
export const appMarkdownHost: MarkdownHost = {
  imageSource: (url) => imageRequest(url, endpoints.apiUrl, api.getToken()),
  openLink: (url) => {
    const id = linkedAttachmentId(url);
    if (id) void openAttachment(id).catch(() => {});
    else if (/^(https?|mailto):/i.test(url)) void Linking.openURL(url);
  },
};
