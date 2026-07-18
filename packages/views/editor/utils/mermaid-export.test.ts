import { describe, expect, it } from "vitest";
import { buildExportSvg, diagramFilenameStem } from "./mermaid-export";

const BASE_OPTIONS = {
  background: "rgb(240, 240, 240)",
  fontFamily: "Inter, sans-serif",
  width: 800,
  height: 600,
};

function parse(markup: string): SVGElement {
  const doc = new DOMParser().parseFromString(markup, "image/svg+xml");
  return doc.documentElement as unknown as SVGElement;
}

describe("buildExportSvg", () => {
  it("replaces Mermaid's percentage width with concrete pixels an <img> can size against", () => {
    // Mermaid renders for embedding: width="100%" has no basis in a file, and
    // the PNG path's <img> would have no intrinsic size to draw.
    const result = buildExportSvg(
      '<svg width="100%" viewBox="0 0 800 600"><g/></svg>',
      BASE_OPTIONS,
    );

    const root = parse(result!);
    expect(root.getAttribute("width")).toBe("800");
    expect(root.getAttribute("height")).toBe("600");
  });

  it("drops the host-tuned max-width so the export is not clipped to the old container", () => {
    const result = buildExportSvg(
      '<svg style="max-width: 320px;" viewBox="0 0 800 600"><g/></svg>',
      BASE_OPTIONS,
    );

    expect(result).not.toContain("max-width");
  });

  it("paints an opaque background so the export is not transparent-on-transparent", () => {
    const result = buildExportSvg('<svg viewBox="0 0 800 600"><g/></svg>', BASE_OPTIONS);

    const root = parse(result!);
    const rect = root.firstElementChild;
    expect(rect?.tagName.toLowerCase()).toBe("rect");
    expect(rect?.getAttribute("fill")).toBe("rgb(240, 240, 240)");
  });

  it("covers a negative-origin viewBox, so padded diagrams do not export with transparent margins", () => {
    // Mermaid emits negative viewBox origins for diagrams with padding; a
    // 0,0-anchored background rect would leave those margins unpainted.
    const result = buildExportSvg('<svg viewBox="-8 -12 800 600"><g/></svg>', BASE_OPTIONS);

    const rect = parse(result!).firstElementChild;
    expect(rect?.getAttribute("x")).toBe("-8");
    expect(rect?.getAttribute("y")).toBe("-12");
    expect(rect?.getAttribute("width")).toBe("800");
    expect(rect?.getAttribute("height")).toBe("600");
  });

  it("falls back to the given size when the viewBox is missing or malformed", () => {
    const rect = parse(buildExportSvg("<svg><g/></svg>", BASE_OPTIONS)!).firstElementChild;
    expect(rect?.getAttribute("width")).toBe("800");
    expect(rect?.getAttribute("height")).toBe("600");

    const bad = parse(buildExportSvg('<svg viewBox="nonsense"><g/></svg>', BASE_OPTIONS)!);
    expect(bad.firstElementChild?.getAttribute("width")).toBe("800");
  });

  it("resolves font-family, which has nothing to inherit from in a standalone file", () => {
    const result = buildExportSvg('<svg viewBox="0 0 800 600"><g/></svg>', BASE_OPTIONS);

    expect(result).toContain("Inter, sans-serif");
  });

  it("declares the SVG namespace so the file opens outside a browser document", () => {
    const result = buildExportSvg('<svg viewBox="0 0 800 600"><g/></svg>', BASE_OPTIONS);

    expect(result).toContain("http://www.w3.org/2000/svg");
  });

  it("preserves the diagram content", () => {
    const result = buildExportSvg(
      '<svg viewBox="0 0 800 600"><text>Start</text></svg>',
      BASE_OPTIONS,
    );

    expect(result).toContain("Start");
  });

  it("returns null on unparseable markup instead of emitting a corrupt file", () => {
    expect(buildExportSvg("<svg><unclosed>", BASE_OPTIONS)).toBeNull();
  });

  it("returns null when the root is not an <svg>", () => {
    expect(buildExportSvg("<div>not a diagram</div>", BASE_OPTIONS)).toBeNull();
  });
});

describe("diagramFilenameStem", () => {
  it("derives the stem from the first meaningful line", () => {
    expect(diagramFilenameStem("graph LR\n  A --> B")).toBe("graph-lr");
  });

  it("skips %% comments and blank lines to reach the real declaration", () => {
    expect(diagramFilenameStem("\n%% title: ignored\n\nsequenceDiagram\n  A->>B: hi")).toBe(
      "sequencediagram",
    );
  });

  it("falls back to 'diagram' when nothing usable survives slugification", () => {
    expect(diagramFilenameStem("")).toBe("diagram");
    expect(diagramFilenameStem("%%%%")).toBe("diagram");
    expect(diagramFilenameStem("中文标题")).toBe("diagram");
  });

  it("caps length and leaves no trailing separator for the extension to butt against", () => {
    const stem = diagramFilenameStem("graph LR with an extremely long trailing description here");

    expect(stem.length).toBeLessThanOrEqual(40);
    expect(stem.endsWith("-")).toBe(false);
  });
});
