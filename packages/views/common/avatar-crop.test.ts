import { describe, it, expect } from "vitest";
import { blobToAvatarFile } from "./avatar-crop";

describe("blobToAvatarFile", () => {
  const blob = new Blob(["x"], { type: "image/webp" });

  it("maps the output type to the matching extension", () => {
    expect(blobToAvatarFile(blob, "photo.png", "image/webp").name).toBe(
      "photo.webp",
    );
    expect(blobToAvatarFile(blob, "photo.png", "image/jpeg").name).toBe(
      "photo.jpg",
    );
    expect(blobToAvatarFile(blob, "photo.gif", "image/png").name).toBe(
      "photo.png",
    );
  });

  it("strips only the final extension from the source name", () => {
    expect(blobToAvatarFile(blob, "my.avatar.jpeg", "image/webp").name).toBe(
      "my.avatar.webp",
    );
    expect(blobToAvatarFile(blob, "no-extension", "image/webp").name).toBe(
      "no-extension.webp",
    );
  });

  it("falls back to a stable base name when the source has none", () => {
    expect(blobToAvatarFile(blob, "", "image/jpeg").name).toBe("avatar.jpg");
    expect(blobToAvatarFile(blob, ".png", "image/jpeg").name).toBe("avatar.jpg");
  });

  it("carries the output mime type onto the File", () => {
    expect(blobToAvatarFile(blob, "photo.png", "image/webp").type).toBe(
      "image/webp",
    );
  });
});
