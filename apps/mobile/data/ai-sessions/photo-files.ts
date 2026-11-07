import { ImageManipulator, SaveFormat } from "expo-image-manipulator";
import type { PhotoPreparation } from "./photo-attachments";

export const photoPreparation: PhotoPreparation = {
  async read(uri) {
    const blob = await (await fetch(uri)).blob();
    // Read only the signature; never serialize the photo as Base64 in JS.
    const header = await new Promise<Uint8Array>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () =>
        reader.result instanceof ArrayBuffer
          ? resolve(new Uint8Array(reader.result))
          : reject(new Error("unreadable"));
      reader.onerror = () => reject(reader.error);
      reader.readAsArrayBuffer(blob.slice(0, 32));
    });
    return { size: blob.size, header };
  },
  async decode(uri, convert) {
    const context = ImageManipulator.manipulate(uri);
    try {
      const image = await context.renderAsync();
      try {
        if (image.width <= 0 || image.height <= 0)
          throw new Error("unreadable");
        return convert
          ? (await image.saveAsync({ format: SaveFormat.JPEG, compress: 1 }))
              .uri
          : uri;
      } finally {
        image.release();
      }
    } finally {
      context.release();
    }
  },
};
