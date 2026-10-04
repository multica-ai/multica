/**
 * Picker + upload glue for the markdown toolbar's image / file buttons.
 *
 * Each call:
 *   1. Opens the appropriate picker (image library / document picker) via
 *      lib/media-picker (MulticaMediaPicker TurboModule on this platform).
 *   2. On user-cancel (or a missing native module), resolves null (caller
 *      should treat as no-op — do not insert anything into the text).
 *   3. Otherwise, streams the file to `/api/upload-file` via
 *      `api.uploadFile`, returning `{ url, filename }` for the caller to
 *      compose into the markdown insertion (`![](url)` or
 *      `[📎 name](url)`).
 *   4. On any failure (network / 4xx / 5xx), shows an Alert and resolves
 *      null. Caller treats null as no-op so nothing partial ends up in
 *      the text.
 *
 * The hook tracks an `uploading` flag so callers can disable the toolbar
 * during an in-flight upload (prevents double-pick + double-insert).
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import {
  filenameFromUri,
  mimeTypeFromFilename,
  pickImage,
  pickDocument,
} from "@/lib/media-picker";
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
  /** The OHOS picker seam cannot report a byte size; when undefined the
   *  pre-upload MAX_FILE_SIZE check is skipped and the server's own limit
   *  applies. */
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
      const uri = await pickImage();
      if (!uri) return null;
      // The picker seam returns only a cache URI — derive the display
      // filename and MIME from it so the multipart Content-Disposition is
      // never empty (mirrors expo's fileName/mimeType fallbacks).
      const name = filenameFromUri(uri);
      return upload(
        { uri, name, type: mimeTypeFromFilename(name, "image/jpeg") },
        ctx,
      );
    },
    [upload],
  );

  const pickAndUploadFile = useCallback(
    async (ctx?: UploadContext): Promise<FileAttachResult | null> => {
      const uri = await pickDocument();
      if (!uri) return null;
      const name = filenameFromUri(uri);
      return upload(
        {
          uri,
          name,
          type: mimeTypeFromFilename(name, "application/octet-stream"),
        },
        ctx,
      );
    },
    [upload],
  );

  return { pickAndUploadImage, pickAndUploadFile, uploading };
}
