/**
 * Picker + upload glue for the markdown toolbar's image / file buttons.
 *
 * Each call:
 *   1. Opens the appropriate picker (image library / document picker).
 *   2. On user-cancel, resolves null (caller should treat as no-op — do not
 *      insert anything into the text).
 *   3. Otherwise, streams the file to `/api/upload-file` via
 *      `api.uploadFile`, returning `{ url, filename }` for the caller to
 *      compose into the markdown insertion (`![](url)` or
 *      `[📎 name](url)`).
 *   4. On any failure (size limit / network / 4xx / 5xx), shows an Alert
 *      and resolves null. Caller treats null as no-op so nothing partial
 *      ends up in the text.
 *
 * The hook tracks an `uploading` flag so callers can disable the toolbar
 * during an in-flight upload (prevents double-pick + double-insert).
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import * as ImagePicker from "expo-image-picker";
import * as DocumentPicker from "expo-document-picker";
import { api, MAX_FILE_SIZE, type FileAsset } from "@/data/api";

export interface FileAttachResult {
  /** Attachment id from the server. Callers MUST carry this to the mutation
   *  that creates / updates the comment, so the backend can re-parent the
   *  attachment from "issue-scoped" to "comment-scoped" (otherwise the
   *  attachment lives at the issue level forever and never cascades on
   *  comment delete). */
  id: string;
  url: string;
  filename: string;
}

export interface UploadContext {
  issueId?: string;
  commentId?: string;
}

interface PickedAsset extends FileAsset {
  size?: number;
}

export function useFileAttach() {
  const [uploading, setUploading] = useState(false);

  const upload = useCallback(
    async (
      asset: PickedAsset,
      ctx?: UploadContext,
    ): Promise<FileAttachResult | null> => {
      if (asset.size != null && asset.size > MAX_FILE_SIZE) {
        Alert.alert(
          "File too large",
          "Files must be smaller than 100 MB.",
        );
        return null;
      }
      setUploading(true);
      try {
        const attachment = await api.uploadFile(asset, ctx);
        return {
          id: attachment.id,
          url: attachment.url,
          filename: attachment.filename,
        };
      } catch (err) {
        Alert.alert(
          "Upload failed",
          err instanceof Error ? err.message : "Unknown error",
        );
        return null;
      } finally {
        setUploading(false);
      }
    },
    [],
  );

  const pickAndUploadImage = useCallback(
    async (ctx?: UploadContext): Promise<FileAttachResult | null> => {
      const result = await ImagePicker.launchImageLibraryAsync({
        // SDK 55: `MediaTypeOptions.Images` is supported (deprecation only
        // hits SDK 56+). Stick with it until we upgrade.
        mediaTypes: ImagePicker.MediaTypeOptions.Images,
        quality: 1,
      });
      if (result.canceled) return null;
      const picked = result.assets[0];
      if (!picked) return null;
      // expo-image-picker exposes `fileName` (camelCase) on iOS;
      // fall back to a placeholder so the multipart Content-Disposition
      // is never empty.
      const asset: PickedAsset = {
        uri: picked.uri,
        name: picked.fileName ?? `image-${Date.now()}.jpg`,
        type: picked.mimeType ?? "image/jpeg",
        size: picked.fileSize,
      };
      return upload(asset, ctx);
    },
    [upload],
  );

  const pickAndUploadFile = useCallback(
    async (ctx?: UploadContext): Promise<FileAttachResult | null> => {
      const result = await DocumentPicker.getDocumentAsync({
        type: "*/*",
        copyToCacheDirectory: true,
      });
      if (result.canceled) return null;
      const picked = result.assets[0];
      if (!picked) return null;
      const asset: PickedAsset = {
        uri: picked.uri,
        name: picked.name,
        type: picked.mimeType ?? "application/octet-stream",
        size: picked.size,
      };
      return upload(asset, ctx);
    },
    [upload],
  );

  return { pickAndUploadImage, pickAndUploadFile, uploading };
}
