import { describe, it, expect } from "vitest";
import { parseTrayImages, combineBodyAndTray } from "./parse-tray-images";
import { serializeTrayImages, type ImageTrayItem } from "./use-image-tray";

function completed(url: string, filename = "a.png"): ImageTrayItem {
  return {
    localId: url,
    blobUrl: "",
    filename,
    status: "completed",
    uploadedUrl: url,
  };
}

describe("parseTrayImages", () => {
  it("returns the whole markdown as body when there is no trailing image block", () => {
    const { body, images } = parseTrayImages("Just some text.\nSecond line.");
    expect(body).toBe("Just some text.\nSecond line.");
    expect(images).toEqual([]);
  });

  it("lifts a trailing block of tray images out of the body in order", () => {
    const md = [
      "Here is the description.",
      "",
      "![image 1](https://cdn/one.png)",
      "![image 2](https://cdn/two.jpg)",
    ].join("\n");
    const { body, images } = parseTrayImages(md);
    expect(body).toBe("Here is the description.");
    expect(images.map((i) => i.url)).toEqual([
      "https://cdn/one.png",
      "https://cdn/two.jpg",
    ]);
    expect(images.map((i) => i.filename)).toEqual(["one.png", "two.jpg"]);
  });

  it("ignores trailing blank lines after the image block", () => {
    const md = "Body\n\n![image 1](https://cdn/a.png)\n\n";
    const { body, images } = parseTrayImages(md);
    expect(body).toBe("Body");
    expect(images).toHaveLength(1);
  });

  it("leaves an inline image (non-tray alt text) in the body", () => {
    const md = "Text\n\n![a screenshot](https://cdn/a.png)";
    const { body, images } = parseTrayImages(md);
    expect(body).toBe(md);
    expect(images).toEqual([]);
  });

  it("handles a body that is only tray images", () => {
    const md = "![image 1](https://cdn/a.png)\n![image 2](https://cdn/b.png)";
    const { body, images } = parseTrayImages(md);
    expect(body).toBe("");
    expect(images).toHaveLength(2);
  });

  it("derives a filename from a URL with query params", () => {
    const { images } = parseTrayImages(
      "x\n\n![image 1](https://cdn/photo%20one.png?w=64)",
    );
    expect(images[0]?.filename).toBe("photo one.png");
  });
});

describe("combineBodyAndTray", () => {
  it("appends tray markdown after a blank line", () => {
    expect(combineBodyAndTray("Body", "![image 1](u)")).toBe(
      "Body\n\n![image 1](u)",
    );
  });

  it("returns just the tray when the body is empty", () => {
    expect(combineBodyAndTray("", "![image 1](u)")).toBe("![image 1](u)");
  });

  it("returns just the body when there are no tray images", () => {
    expect(combineBodyAndTray("Body", "")).toBe("Body");
  });
});

describe("round trip parse ↔ serialize", () => {
  it("reproduces the saved content", () => {
    const items = [completed("https://cdn/one.png"), completed("https://cdn/two.png")];
    const { markdown } = serializeTrayImages(items);
    const saved = combineBodyAndTray("The body text.", markdown);

    const { body, images } = parseTrayImages(saved);
    expect(body).toBe("The body text.");
    const reserialized = serializeTrayImages(
      images.map((i) => completed(i.url, i.filename)),
    );
    expect(combineBodyAndTray(body, reserialized.markdown)).toBe(saved);
  });
});
