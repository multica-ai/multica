import type { Attachment } from "@multica/core/types";
import type { ApiClient } from "@multica/core/api/client";

export type MobileUploadAsset = {
  uri: string;
  name: string;
  mimeType?: string;
  size?: number;
};

export function uploadMobileAsset(
  api: ApiClient,
  asset: MobileUploadAsset,
  context?: { issueId?: string; commentId?: string },
): Promise<Attachment> {
  const file = {
    uri: asset.uri,
    name: asset.name,
    type: asset.mimeType ?? "application/octet-stream",
    size: asset.size ?? 0,
  } as unknown as File;

  return api.uploadFile(file, context);
}
