import type { ImagePickerOptions } from "expo-image-picker";
import type { UploadAsset } from "./uploads";

export const MAX_PHOTOS = 6;
export const MAX_PHOTO_BYTES = 5 << 20;
export const MAX_ATTACHMENTS = 20;

type NamedFile = { name: string; mimeType?: string | null };
export type PhotoErrorCode =
  | "unsupported"
  | "unreadable"
  | "too_large"
  | "photo_limit"
  | "attachment_limit";
export class PhotoAttachmentError extends Error {
  constructor(public readonly code: PhotoErrorCode) {
    super(code);
  }
}

export function isPhoto(file: NamedFile) {
  return (
    file.mimeType?.startsWith("image/") === true ||
    /\.(jpe?g|png|heic|heif|gif|webp|avif|bmp|tiff?)$/i.test(file.name)
  );
}

export function photoPickerOptions(
  files: readonly NamedFile[],
): ImagePickerOptions | null {
  const remaining = Math.min(
    MAX_PHOTOS - files.filter(isPhoto).length,
    MAX_ATTACHMENTS - files.length,
  );
  // Zero means unlimited in PHPicker, rather than no remaining capacity.
  return remaining > 0
    ? {
        mediaTypes: ["images"],
        allowsMultipleSelection: true,
        orderedSelection: true,
        selectionLimit: remaining,
        allowsEditing: false,
        quality: 1,
        shouldDownloadFromNetwork: true,
      }
    : null;
}

export function imageType(
  header: Uint8Array,
): "image/jpeg" | "image/png" | "image/heic" | null {
  if (header[0] === 0xff && header[1] === 0xd8 && header[2] === 0xff)
    return "image/jpeg";
  if (
    [137, 80, 78, 71, 13, 10, 26, 10].every(
      (byte, index) => header[index] === byte,
    )
  )
    return "image/png";
  const text = String.fromCharCode(...header);
  if (
    text.slice(4, 8) === "ftyp" &&
    /heic|heix|hevc|hevx|mif1|msf1/.test(text.slice(8)) &&
    !/avif|avis/.test(text.slice(8))
  )
    return "image/heic";
  return null;
}

export interface PhotoPreparation {
  read(uri: string): Promise<{ size: number; header: Uint8Array }>;
  decode(uri: string, convert: boolean): Promise<string>;
}

/** Validate the bytes we will send, including the result of HEIC conversion. */
export async function preparePickedAttachment(
  asset: UploadAsset,
  photosOnly: boolean,
  deps: PhotoPreparation,
): Promise<UploadAsset> {
  try {
    let info = await deps.read(asset.uri);
    if (info.size <= 0) throw new PhotoAttachmentError("unreadable");
    const type = imageType(info.header);
    if (!type) {
      if (photosOnly || isPhoto(asset))
        throw new PhotoAttachmentError("unsupported");
      return asset;
    }
    if (type !== "image/heic" && info.size > MAX_PHOTO_BYTES)
      throw new PhotoAttachmentError("too_large");
    const uri = await deps.decode(asset.uri, type === "image/heic");
    if (type === "image/heic") {
      info = await deps.read(uri);
      if (info.size <= 0 || imageType(info.header) !== "image/jpeg")
        throw new PhotoAttachmentError("unreadable");
    }
    if (info.size > MAX_PHOTO_BYTES)
      throw new PhotoAttachmentError("too_large");
    const mimeType = type === "image/heic" ? "image/jpeg" : type;
    const suffix = mimeType === "image/png" ? ".png" : ".jpg";
    const stem = asset.name.replace(/\.[^.]+$/, "") || "photo";
    return { uri, name: `${stem}${suffix}`, mimeType };
  } catch (error) {
    throw error instanceof PhotoAttachmentError
      ? error
      : new PhotoAttachmentError("unreadable");
  }
}

/** The picker may return after navigation; never append into a different draft. */
export async function importPickedAttachments(
  assets: readonly UploadAsset[],
  photosOnly: boolean,
  deps: PhotoPreparation & {
    isCurrent(): boolean;
    existing(): readonly NamedFile[];
    accept(asset: UploadAsset): void;
  },
) {
  const errors: { name: string; code: PhotoErrorCode }[] = [];
  for (const asset of assets) {
    if (!deps.isCurrent()) break;
    try {
      if (deps.existing().length >= MAX_ATTACHMENTS)
        throw new PhotoAttachmentError("attachment_limit");
      const file = await preparePickedAttachment(asset, photosOnly, deps);
      if (!deps.isCurrent()) break;
      if (isPhoto(file) && deps.existing().filter(isPhoto).length >= MAX_PHOTOS)
        throw new PhotoAttachmentError("photo_limit");
      deps.accept(file);
    } catch (error) {
      if (!deps.isCurrent()) break;
      errors.push({
        name: asset.name,
        code: error instanceof PhotoAttachmentError ? error.code : "unreadable",
      });
    }
  }
  return errors;
}
