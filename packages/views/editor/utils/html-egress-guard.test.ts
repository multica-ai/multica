import { describe, expect, it } from "vitest";
import { guardedContent } from "../../test/guarded-preview";
import {
  HTML_PREVIEW_CSP,
  PREVIEW_POLICY_HEAD,
  buildGuardedPreviewDocument,
  withPreviewPolicy,
} from "./html-egress-guard";

const parse = (html: string) => new DOMParser().parseFromString(html, "text/html");
const policyOf = (doc: Document) =>
  doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute("content");

function frameOf(srcdoc: string) {
  const host = document.createElement("iframe");
  host.setAttribute("srcdoc", srcdoc);
  return host;
}

describe("html preview egress guard", () => {
  it("gives the document no network of its own", () => {
    const directives = new Map(
      HTML_PREVIEW_CSP.split("; ").map((d) => [d.split(" ")[0], d.split(" ").slice(1)]),
    );
    expect(directives.get("default-src")).toEqual(["'none'"]);
    expect(directives.get("connect-src")).toEqual(["'none'"]);
    expect(directives.get("form-action")).toEqual(["'none'"]);
    expect(directives.get("frame-src")).toEqual(["'none'"]);
    expect(directives.get("base-uri")).toEqual(["'none'"]);
    // Images and media may only be inline: an image URL is a way out.
    expect(directives.get("img-src")).toEqual(["data:", "blob:"]);
    // The only origin anywhere is jsDelivr's npm path, for libraries.
    const origins = [...directives.values()].flat().filter((s) => s.includes("://"));
    expect(new Set(origins)).toEqual(new Set(["https://cdn.jsdelivr.net/npm/"]));
    expect(HTML_PREVIEW_CSP).not.toContain("'self'");
  });

  it("puts the policy before the author's markup, after a leading doctype", () => {
    expect(withPreviewPolicy("<p>hi</p>")).toBe(PREVIEW_POLICY_HEAD + "<p>hi</p>");
    expect(withPreviewPolicy("<!DOCTYPE html><html><head><meta http-equiv='refresh'></head></html>")).toBe(
      "<!DOCTYPE html>" + PREVIEW_POLICY_HEAD + "<html><head><meta http-equiv='refresh'></head></html>",
    );
  });

  it("frames the document, with its policy, in a wrapper that refuses its navigations", () => {
    const html = `<p title="a&quot;b">x & y</p><script>location = "https://x.example/?" + 1</script>`;
    const wrapper = parse(buildGuardedPreviewDocument(html, 'Report "Q3"'));
    expect(policyOf(wrapper)).toBe(HTML_PREVIEW_CSP);
    const inner = wrapper.getElementById("f")!;
    expect(inner.tagName).toBe("IFRAME");
    expect(inner.getAttribute("sandbox")).toBe("allow-scripts");
    expect(inner.getAttribute("title")).toBe('Report "Q3"');
    // Escaped as an attribute and read back byte for byte.
    expect(inner.getAttribute("srcdoc")).toBe(withPreviewPolicy(html));
    expect(guardedContent(frameOf(buildGuardedPreviewDocument(html, "t")))).toBe(html);
  });

  it("can't be broken out of by the author's markup", () => {
    const html = `"></iframe><script>parent.leak = 1</script><iframe srcdoc="`;
    const wrapper = parse(buildGuardedPreviewDocument(html, "t"));
    expect(wrapper.querySelectorAll("iframe")).toHaveLength(1);
    // The wrapper's only script is its relay.
    expect(wrapper.querySelectorAll("script")).toHaveLength(1);
    expect(wrapper.querySelector("script")?.textContent).not.toContain("leak");
    expect(guardedContent(frameOf(buildGuardedPreviewDocument(html, "t")))).toBe(html);
  });

  it("relays messages only between the app and the framed document", () => {
    const relay = parse(buildGuardedPreviewDocument("", "t")).querySelector("script")!.textContent!;
    expect(relay).toContain("e.source===w)parent.postMessage(e.data,'*')");
    expect(relay).toContain("e.source===parent)w.postMessage(e.data,'*')");
  });
});
