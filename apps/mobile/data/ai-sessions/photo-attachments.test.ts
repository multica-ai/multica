// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  imageType,
  importPickedAttachments,
  isPhoto,
  MAX_PHOTO_BYTES,
  photoPickerOptions,
  preparePickedAttachment,
  type PhotoPreparation,
} from "./photo-attachments";

const jpeg = new Uint8Array([255, 216, 255, 224]);
const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]);
const heic = Uint8Array.from([
  0,
  0,
  0,
  24,
  ...Array.from("ftypheic\0\0\0\0mif1").map((char) => char.charCodeAt(0)),
]);
const photo = {
  uri: "file:///photo",
  name: "photo.jpg",
  mimeType: "image/jpeg",
};
const deps = (header = jpeg, size = 100): PhotoPreparation => ({
  read: vi.fn(async () => ({ header, size })),
  decode: vi.fn(async (uri) => uri),
});

describe("photo selection", () => {
  it("limits additional selections across both entry points without passing zero", () => {
    const five = Array.from({ length: 5 }, () => photo);
    expect(photoPickerOptions(five)).toMatchObject({
      selectionLimit: 1,
      orderedSelection: true,
      mediaTypes: ["images"],
      shouldDownloadFromNetwork: true,
    });
    expect(photoPickerOptions([...five, photo])).toBeNull();
    expect(
      photoPickerOptions(
        Array.from({ length: 20 }, () => ({ name: "notes.txt" })),
      ),
    ).toBeNull();
    expect(
      photoPickerOptions([...five, { name: "notes.txt" }])?.selectionLimit,
    ).toBe(1);
    expect(isPhoto({ name: "HEIC.HEIC" })).toBe(true);
  });

  it("identifies bytes rather than trusting the suffix or MIME", async () => {
    expect(imageType(jpeg)).toBe("image/jpeg");
    expect(imageType(png)).toBe("image/png");
    expect(imageType(heic)).toBe("image/heic");
    expect(imageType(new Uint8Array([1, 2]))).toBeNull();
    await expect(
      preparePickedAttachment({ ...photo, name: "wrong.jpg" }, true, deps(png)),
    ).resolves.toMatchObject({ name: "wrong.png", mimeType: "image/png" });
    await expect(
      preparePickedAttachment(photo, true, deps(new Uint8Array([1, 2]))),
    ).rejects.toMatchObject({ code: "unsupported" });
  });

  it("accepts exactly 5 MiB and rejects larger, empty and undecodable images", async () => {
    await expect(
      preparePickedAttachment(photo, true, deps(jpeg, MAX_PHOTO_BYTES)),
    ).resolves.toEqual(photo);
    const tooBig = deps(jpeg, MAX_PHOTO_BYTES + 1);
    await expect(
      preparePickedAttachment(photo, true, tooBig),
    ).rejects.toMatchObject({ code: "too_large" });
    expect(tooBig.decode).not.toHaveBeenCalled();
    await expect(
      preparePickedAttachment(photo, true, deps(jpeg, 0)),
    ).rejects.toMatchObject({ code: "unreadable" });
    await expect(
      preparePickedAttachment(photo, true, {
        ...deps(),
        decode: async () => {
          throw new Error("decode_failed");
        },
      }),
    ).rejects.toMatchObject({ code: "unreadable" });
  });

  it("validates converted HEIC bytes and size and never relabels an HEIC original", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({ header: heic, size: 600 })
      .mockResolvedValueOnce({ header: jpeg, size: 400 });
    const decode = vi.fn(async () => "file:///converted");
    await expect(
      preparePickedAttachment({ ...photo, name: "IMG.HEIC" }, true, {
        read,
        decode,
      }),
    ).resolves.toEqual({
      uri: "file:///converted",
      name: "IMG.jpg",
      mimeType: "image/jpeg",
    });
    expect(decode).toHaveBeenCalledWith(photo.uri, true);
    for (const info of [
      { header: heic, size: 300 },
      { header: jpeg, size: MAX_PHOTO_BYTES + 1 },
    ]) {
      const readResult = vi
        .fn()
        .mockResolvedValueOnce({ header: heic, size: 100 })
        .mockResolvedValueOnce(info);
      await expect(
        preparePickedAttachment(photo, true, { read: readResult, decode }),
      ).rejects.toMatchObject({
        code: info.header === heic ? "unreadable" : "too_large",
      });
    }
  });

  it("retains ordinary files and rejects unsupported image formats", async () => {
    const file = {
      uri: "file:///notes",
      name: "notes.txt",
      mimeType: "text/plain",
    };
    const plain = deps(new Uint8Array([65, 66]));
    await expect(preparePickedAttachment(file, false, plain)).resolves.toEqual(
      file,
    );
    expect(plain.decode).not.toHaveBeenCalled();
    await expect(
      preparePickedAttachment({ ...file, name: "animation.gif" }, false, plain),
    ).rejects.toMatchObject({ code: "unsupported" });
  });

  it("keeps legal items in selection order and reports each rejected item", async () => {
    const files = [photo];
    const bad = { ...photo, uri: "bad", name: "bad.gif" };
    const errors = await importPickedAttachments(
      [photo, bad, { ...photo, name: "second.jpg" }],
      true,
      {
        ...deps(),
        read: async (uri) => ({
          size: 100,
          header: uri === "bad" ? new Uint8Array() : jpeg,
        }),
        existing: () => files,
        isCurrent: () => true,
        accept: (file) => files.push(file as typeof photo),
      },
    );
    expect(errors).toEqual([{ name: "bad.gif", code: "unsupported" }]);
    expect(files.map((file) => file.name)).toEqual([
      "photo.jpg",
      "photo.jpg",
      "second.jpg",
    ]);
  });

  it("enforces quotas again after processing and does not append after navigation", async () => {
    const accept = vi.fn();
    const files = Array.from({ length: 6 }, () => photo);
    expect(
      await importPickedAttachments([photo], false, {
        ...deps(),
        existing: () => files,
        isCurrent: () => true,
        accept,
      }),
    ).toEqual([{ name: photo.name, code: "photo_limit" }]);
    let current = true;
    await importPickedAttachments([photo, photo], true, {
      ...deps(),
      decode: async (uri) => {
        current = false;
        return uri;
      },
      existing: () => [],
      isCurrent: () => current,
      accept,
    });
    expect(accept).not.toHaveBeenCalled();
  });
});
