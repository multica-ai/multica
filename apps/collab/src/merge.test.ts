import "./dom";
import { describe, expect, it } from "vitest";
import { createDocumentCodec, type JSONContent } from "@multica/views/editor/schema/codec";
import { keepLiveBlocks } from "./merge";

const codec = createDocumentCodec();
const p = (text: string): JSONContent => ({ type: "paragraph", content: [{ type: "text", text }] });
const upload = (id: string): JSONContent => ({ type: "fileCard", attrs: { href: "", filename: id, uploading: true, uploadId: id } });
const doc = (...content: JSONContent[]): JSONContent => ({ type: "doc", content });
const names = (json: JSONContent) =>
  json.content!.map((block) => (block.attrs?.uploading ? `upload:${block.attrs.uploadId}` : codec.serialize(doc(block))));

describe("keepLiveBlocks", () => {
  it("keeps each upload placeholder after the live block it followed", () => {
    const live = doc(upload("u0"), p("A"), upload("u1"), p("B"), upload("u2"), upload("u3"), p("C"));
    const next = doc(p("A"), p("B2"), p("C"), p("D"));
    expect(names(keepLiveBlocks(codec, live, next))).toEqual([
      "upload:u0",
      "A",
      "upload:u1",
      "B2",
      "upload:u2",
      "upload:u3",
      "C",
      "D",
    ]);
  });

  it("pairs a placeholder with the empty paragraph its Markdown parses back into", () => {
    const live = doc(p("A"), upload("u1"), p("B"));
    const next = codec.parse(codec.serialize(live).replace("A", "A2"));
    expect(names(keepLiveBlocks(codec, live, next))).toEqual(["A2", "upload:u1", "B"]);
  });

  it("keeps a placeholder whose live block the writer deleted", () => {
    const live = doc(p("A"), p("B"), upload("u1"), p("C"));
    expect(names(keepLiveBlocks(codec, live, doc(p("A"), p("C"))))).toEqual(["A", "upload:u1", "C"]);
  });

  it("keeps the live copy of an unchanged block and takes the writer's copy of a changed one", () => {
    const sized = (src: string): JSONContent => ({ type: "image", attrs: { src, alt: "", width: 800, height: 600 } });
    const live = doc(sized("https://cdn.example.com/a.png"), sized("https://cdn.example.com/b.png"));
    const next = codec.parse("![](https://cdn.example.com/a.png)\n\n![](https://cdn.example.com/c.png)");
    const [kept, replaced] = keepLiveBlocks(codec, live, next).content!;
    expect(kept!.attrs).toMatchObject({ src: "https://cdn.example.com/a.png", width: 800, height: 600 });
    expect(replaced!.attrs?.src).toBe("https://cdn.example.com/c.png");
    expect(replaced!.attrs?.width ?? null).toBeNull();
  });
});
