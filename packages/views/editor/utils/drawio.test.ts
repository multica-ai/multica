import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DRAWIO_MAX_ENCODED_CHARS,
  buildDrawioViewerSrc,
  encodeDrawioXml,
  forgetDrawioViewerProbe,
  isDrawioXml,
  prepareDrawioEmbed,
  probeDrawioViewer,
  usableDrawioViewerUrl,
} from "./drawio";

const DIAGRAM = `<mxfile><diagram id="d1" name="Page-1"><mxGraphModel><root>
<mxCell id="0"/><mxCell id="1" parent="0"/>
<mxCell id="a" value="Issue → Agent" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="50" as="geometry"/></mxCell>
</root></mxGraphModel></diagram></mxfile>`;

const VIEWER = "https://viewer.example.test";

// Reverse of the viewer's `#R` decoding, so the test checks the wire format
// draw.io reads rather than only our own round trip.
async function decodeLikeViewer(encoded: string): Promise<string> {
  const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
  const stream = new Response(bytes).body!.pipeThrough(
    new DecompressionStream("deflate-raw"),
  );
  return decodeURIComponent(await new Response(stream).text());
}

afterEach(() => {
  vi.unstubAllGlobals();
  forgetDrawioViewerProbe(VIEWER);
});

describe("isDrawioXml", () => {
  it.each([
    ["an mxfile document", DIAGRAM, true],
    ["a bare graph model", "<mxGraphModel><root/></mxGraphModel>", true],
    ["surrounding whitespace", `\n  ${DIAGRAM}\n`, true],
    ["malformed XML", "<mxfile><diagram></mxfile>", false],
    ["XML that is not a diagram", "<svg><rect/></svg>", false],
    ["plain text", "flowchart TD; A-->B", false],
    ["an empty body", "", false],
  ])("%s → %s", (_label, xml, expected) => {
    expect(isDrawioXml(xml)).toBe(expected);
  });
});

describe("encodeDrawioXml", () => {
  it("produces the payload the viewer decodes back to the same XML", async () => {
    const encoded = await encodeDrawioXml(DIAGRAM);
    expect(encoded).toMatch(/^[A-Za-z0-9+/]+=*$/);
    await expect(decodeLikeViewer(encoded)).resolves.toBe(DIAGRAM);
  });
});

describe("buildDrawioViewerSrc", () => {
  it("keeps the diagram in the fragment, never the query", () => {
    const src = new URL(buildDrawioViewerSrc(`${VIEWER}/drawio`, "a+b/c="));
    expect(src.origin).toBe(VIEWER);
    expect(src.pathname).toBe("/drawio");
    expect(src.searchParams.get("lightbox")).toBe("1");
    expect(src.searchParams.get("nav")).toBe("1");
    expect(src.hash).toBe(`#R${encodeURIComponent("a+b/c=")}`);
  });
});

describe("usableDrawioViewerUrl", () => {
  const app = "https://app.example.test";
  it.each([
    ["the configured viewer", VIEWER, VIEWER],
    ["nothing configured", "", null],
    ["a relative URL", "/drawio", null],
    ["a non-http scheme", "javascript:alert(1)", null],
    ["the app's own origin", `${app}/drawio`, null],
  ])("%s", (_label, configured, expected) => {
    expect(usableDrawioViewerUrl(configured, app)).toBe(expected);
  });
});

describe("probeDrawioViewer", () => {
  it("asks each origin once and reuses the answer", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null));
    vi.stubGlobal("fetch", fetchMock);
    await expect(probeDrawioViewer(VIEWER)).resolves.toBe(true);
    await expect(probeDrawioViewer(`${VIEWER}/other`)).resolves.toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({
      mode: "no-cors",
      credentials: "omit",
      referrerPolicy: "no-referrer",
    });
  });

  it("reports a network failure as unreachable and asks again after a reset", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(new Response(null));
    vi.stubGlobal("fetch", fetchMock);
    await expect(probeDrawioViewer(VIEWER)).resolves.toBe(false);
    forgetDrawioViewerProbe(VIEWER);
    await expect(probeDrawioViewer(VIEWER)).resolves.toBe(true);
  });

  it("gives up after the timeout", async () => {
    vi.useFakeTimers();
    try {
      vi.stubGlobal(
        "fetch",
        vi.fn((_url: string, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            init.signal?.addEventListener("abort", () => reject(new Error("aborted")));
          }),
        ),
      );
      const probe = probeDrawioViewer(VIEWER, 50);
      await vi.advanceTimersByTimeAsync(50);
      await expect(probe).resolves.toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("prepareDrawioEmbed", () => {
  it("returns the viewer src for a valid diagram", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null)));
    const result = await prepareDrawioEmbed(DIAGRAM, VIEWER);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.src.startsWith(`${VIEWER}/?lightbox=1`)).toBe(true);
  });

  it("rejects malformed XML before contacting the viewer", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(prepareDrawioEmbed("<mxfile>", VIEWER)).resolves.toEqual({
      ok: false,
      error: "invalid",
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects a diagram whose payload exceeds the cap", async () => {
    // Random labels defeat compression, so the payload outgrows the cap.
    const cells = Array.from(
      { length: 9000 },
      (_, i) =>
        `<mxCell id="c${i}" value="${crypto.randomUUID()}${crypto.randomUUID()}" vertex="1" parent="1"/>`,
    ).join("");
    const huge = `<mxGraphModel><root>${cells}</root></mxGraphModel>`;
    expect((await encodeDrawioXml(huge)).length).toBeGreaterThan(DRAWIO_MAX_ENCODED_CHARS);
    await expect(prepareDrawioEmbed(huge, VIEWER)).resolves.toEqual({
      ok: false,
      error: "too_large",
    });
  });

  it("reports an unreachable viewer", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("offline")));
    await expect(prepareDrawioEmbed(DIAGRAM, VIEWER)).resolves.toEqual({
      ok: false,
      error: "unreachable",
    });
  });
});
