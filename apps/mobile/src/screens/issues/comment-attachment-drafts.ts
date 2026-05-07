import type { MobileUploadAsset } from "../../platform/upload";

export type DraftCommentAttachment = MobileUploadAsset & {
  id: string;
};

export function createDraftCommentAttachment(
  asset: MobileUploadAsset,
  index: number,
  now = Date.now(),
): DraftCommentAttachment {
  return {
    ...asset,
    id: `${asset.uri}:${asset.name}:${asset.size ?? 0}:${now}:${index}`,
  };
}
