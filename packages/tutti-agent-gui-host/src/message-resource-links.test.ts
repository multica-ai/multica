// @vitest-environment node
import { expect, it } from "vitest";
import { localResourceTarget, previewResourceHref, remarkMessageResourceLinks } from "./message-resource-links";

it.each([
  ["file:///project/cases.html", "/project/cases.html", undefined],
  ["file://localhost/project/%E6%88%BF%E9%97%B4%20report.html#L12", "/project/房间 report.html", 12],
  ["file:///C:/project/cases.html", "C:/project/cases.html", undefined],
  ["/project/a.ts:24:3", "/project/a.ts", 24],
  ["./output/report.html#L8", "./output/report.html", 8],
  ["../output/report.html", "../output/report.html", undefined],
  ["output/report.html", "output/report.html", undefined],
  ["C:\\project\\report.html", "C:/project/report.html", undefined],
])("resolves preview location %s", (href, path, line) => {
  expect(localResourceTarget(href)).toEqual({ path, ...(line ? { line } : {}) });
  if (/^(?:\/|[a-z]:[\\/])/i.test(path)) {
    expect(localResourceTarget(previewResourceHref(href)!)).toEqual(localResourceTarget(href));
  } else {
    expect(previewResourceHref(href)).toBeNull();
  }
});

it.each(["file://server/project/a.html", "file:///project/%00.html", "file:///project/a.html?query=1", "javascript:alert(1)", "data:text/html,x", "//example.org/a.html", "https://example.org/a.html", "cat /project/a.html", "file:///project/%zz.html", "/project/a\n.html"])("does not treat %s as a local resource", (href) => {
  expect(localResourceTarget(href)).toBeNull();
});

it("recognizes ordinary webpages without turning unsafe schemes into preview links", () => {
  expect(previewResourceHref("https://example.org/docs")).toBe("https://example.org/docs");
  expect(previewResourceHref("https://user:secret@example.org/docs")).toBeNull();
  expect(previewResourceHref("javascript:/project/a.html")).toBeNull();
});

it("links prose and standalone inline paths, preserves punctuation, and leaves code and images intact", () => {
  const code = { type: "code", value: "cat /project/a.html" };
  const command = { type: "inlineCode", value: "cat /project/a.html" };
  const image = { type: "image", url: "file:///project/a.png" };
  const tree: Parameters<ReturnType<typeof remarkMessageResourceLinks>>[0] = { type: "root", children: [{ type: "paragraph", children: [
    { type: "text", value: "确认：file:///project/report(1).html。 源码（/project/a.ts:12）。 output/review.html, javascript:/project/no.html" },
    { type: "inlineCode", value: "output/review.html" }, command, image,
    { type: "link", url: "file:///project/explicit.html", children: [{ type: "text", value: "/project/label.html" }] },
  ] }, code] };
  remarkMessageResourceLinks()(tree);
  const nodes = tree.children![0]!.children!;
  expect(nodes.filter((node) => node.type === "link").map((node) => node.url)).toEqual([
    "/project/report(1).html", "/project/a.ts:12", "/project/explicit.html",
  ]);
  expect(nodes.filter((node) => node.type === "text").map((node) => node.value).join("")).toBe("确认：。 源码（）。 output/review.html, javascript:/project/no.html");
  expect(nodes).toContainEqual(command); expect(nodes).toContainEqual(image); expect(tree.children!.at(-1)).toBe(code);
});

it.each(["AGENTS.md", "notes.txt", "src/example.ts", "./AGENTS.md", "../output/report.html", "目录/说明.md", "C:notes.txt"])("keeps %s out of automatic message links", (value) => {
  const code = { type: "inlineCode", value };
  const tree = { type: "root", children: [{ type: "paragraph", children: [code] }] };
  remarkMessageResourceLinks()(tree);
  expect(tree.children[0]!.children).toEqual([code]);
  expect(previewResourceHref(value)).toBeNull();
});

it.each(["/project/房间.md:12", "C:\\project\\房间.md", "file:///project/房间%20report.md#L12"])("links absolute inline paths: %s", (value) => {
  const code = { type: "inlineCode", value };
  const tree = { type: "root", children: [{ type: "paragraph", children: [code] }] };
  remarkMessageResourceLinks()(tree);
  expect(tree.children[0]!.children).toEqual([{ type: "link", url: previewResourceHref(value), children: [code] }]);
});

it("leaves bare slash commands as text while linking rooted paths", () => {
  const paragraph = { type: "paragraph", children: [{ type: "text", value: "Please run /login, then open /etc/hosts or /note.txt." }] };
  remarkMessageResourceLinks()({ type: "root", children: [paragraph] });
  expect(paragraph.children.filter((node) => node.type === "link").map((node) => (node as { url?: string }).url)).toEqual(["/etc/hosts", "/note.txt"]);
  expect(previewResourceHref("/login")).toBe("/login");
});
