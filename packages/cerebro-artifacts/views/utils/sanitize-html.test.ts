import { describe, it, expect } from "vitest";
import { sanitizeHtml } from "./sanitize-html";

describe("sanitizeHtml", () => {
  it("strips <img onerror=...> handler while keeping the tag", () => {
    const out = sanitizeHtml(`<img src="https://example.com/x.png" onerror="alert(1)" alt="x">`);
    expect(out).not.toMatch(/onerror/i);
    expect(out).not.toMatch(/alert\(1\)/);
    expect(out).toMatch(/<img/i);
    expect(out).toMatch(/src="https:\/\/example\.com\/x\.png"/);
  });

  it("strips javascript: hrefs", () => {
    const out = sanitizeHtml(`<a href="javascript:alert(1)">click</a>`);
    expect(out).not.toMatch(/javascript:/i);
    expect(out).not.toMatch(/alert\(1\)/);
    expect(out).toMatch(/click/);
  });

  it("removes <style> tags entirely", () => {
    const out = sanitizeHtml(`<style>body { background: red; }</style><p>hello</p>`);
    expect(out).not.toMatch(/<style/i);
    expect(out).not.toMatch(/background:\s*red/i);
    expect(out).toMatch(/<p>hello<\/p>/);
  });

  it("removes <script> tags entirely", () => {
    const out = sanitizeHtml(`<p>before</p><script>alert(1)</script><p>after</p>`);
    expect(out).not.toMatch(/<script/i);
    expect(out).not.toMatch(/alert\(1\)/);
    expect(out).toMatch(/<p>before<\/p>/);
    expect(out).toMatch(/<p>after<\/p>/);
  });

  it("removes <iframe> tags", () => {
    const out = sanitizeHtml(`<iframe src="javascript:alert(1)"></iframe><p>ok</p>`);
    expect(out).not.toMatch(/<iframe/i);
    expect(out).toMatch(/<p>ok<\/p>/);
  });

  it("strips style attributes", () => {
    const out = sanitizeHtml(`<p style="position:fixed;top:0">x</p>`);
    expect(out).not.toMatch(/style=/i);
    expect(out).toMatch(/<p>x<\/p>/);
  });

  it("keeps allowed typographic markup intact", () => {
    const html = `<h2>Title</h2><p><strong>bold</strong> and <em>em</em></p><ul><li>one</li></ul>`;
    expect(sanitizeHtml(html)).toBe(html);
  });

  it("keeps http(s) and mailto hrefs", () => {
    expect(sanitizeHtml(`<a href="https://example.com">x</a>`)).toMatch(/href="https:\/\/example\.com"/);
    expect(sanitizeHtml(`<a href="http://example.com">x</a>`)).toMatch(/href="http:\/\/example\.com"/);
    expect(sanitizeHtml(`<a href="mailto:a@b.com">x</a>`)).toMatch(/href="mailto:a@b\.com"/);
  });

  it("strips data: URIs in img src", () => {
    const out = sanitizeHtml(`<img src="data:image/svg+xml;base64,PHN2Zz4=" alt="x">`);
    expect(out).not.toMatch(/data:/i);
  });
});
