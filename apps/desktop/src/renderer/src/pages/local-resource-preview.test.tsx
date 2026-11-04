import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { LocalPreviewFile, LocalPreviewLocation } from "@multica/core/local-conversation/protocol";
import { LocalFilePreview } from "./local-resource-preview";

const { read, openExternal, scrollIntoView } = vi.hoisted(() => ({ read: vi.fn(), openExternal: vi.fn(), scrollIntoView: vi.fn() }));
vi.mock("../platform/local-device", () => ({ useLocalDevice: () => ({ id: "local", transport: {} }) }));
vi.mock("../platform/local-v2-preview", () => ({ localV2Preview: () => ({ read }) }));

const scope = { userId: "user", workspaceId: "workspace" };
const location: LocalPreviewLocation = { root: "/project", path: "/project/README.md", name: "README.md", kind: "file" };
const markdown = "# Documentation\n\nA paragraph.\n\n- First item\n\n[Website](https://example.com) [Local](./private.md)\n\n| Name | Status |\n| --- | --- |\n| Preview | Ready |";
const file: LocalPreviewFile = { ...location, mimeType: "text/plain", size: 200, truncated: false, text: markdown };

beforeEach(() => {
  read.mockResolvedValue(file);
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { openExternal } });
  Element.prototype.scrollIntoView = scrollIntoView;
});
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it.each(["README.md", "GUIDE.MD", "guide.MarkDown"])("renders Markdown and switches both ways for %s", async (name) => {
  render(<QueryClientProvider client={new QueryClient()}><LocalFilePreview location={{ ...location, name, path: `/project/${name}` }} scope={scope} chinese={false} /></QueryClientProvider>);
  expect(await screen.findByRole("heading", { name: "Documentation" })).toBeInTheDocument();
  expect(screen.getByRole("listitem")).toHaveTextContent("First item");
  expect(screen.getByRole("table")).toHaveTextContent("PreviewReady");
  expect(screen.getByText("A paragraph.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Source" }));
  expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  expect(document.querySelector("pre")).toHaveTextContent("# Documentation");
  fireEvent.click(screen.getByRole("button", { name: "Preview" }));
  expect(screen.getByRole("heading", { name: "Documentation" })).toBeInTheDocument();
});

it("opens line references in source, scrolls on returning to source, and resets for another file", async () => {
  const client = new QueryClient();
  const view = (target: LocalPreviewLocation, line?: number) => <QueryClientProvider client={client}><LocalFilePreview location={target} scope={scope} line={line} chinese /></QueryClientProvider>;
  const result = render(view(location, 3));
  await screen.findByRole("button", { name: "预览" });
  expect(document.querySelector("pre .bg-accent")).toHaveTextContent("A paragraph.");
  expect(scrollIntoView).toHaveBeenCalledWith({ block: "center" });
  fireEvent.click(screen.getByRole("button", { name: "预览" }));
  expect(screen.getByRole("heading")).toBeInTheDocument();
  scrollIntoView.mockClear();
  fireEvent.click(screen.getByRole("button", { name: "源码" }));
  expect(scrollIntoView).toHaveBeenCalled();
  result.rerender(view({ ...location, path: "/project/second.md", name: "second.md" }));
  expect(await screen.findByRole("heading")).toBeInTheDocument();
  result.rerender(view(location));
  expect(await screen.findByRole("heading")).toBeInTheDocument();
  result.rerender(view({ ...location, path: "/project/second.md", name: "second.md" }, 5));
  await waitFor(() => expect(document.querySelector("pre .bg-accent")).toHaveTextContent("First item"));
});

it("uses Desktop external navigation for web links and keeps relative files inside the preview boundary", async () => {
  const client = new QueryClient();
  render(<QueryClientProvider client={client}><LocalFilePreview location={location} scope={scope} chinese={false} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole("link", { name: "Website" }));
  expect(openExternal).toHaveBeenCalledWith("https://example.com");
  fireEvent.click(screen.getByRole("link", { name: "Local" }));
  expect(openExternal).toHaveBeenCalledTimes(1);
});

it("handles empty Markdown and keeps truncation notices accurate in both modes", async () => {
  read.mockResolvedValue({ ...file, text: "", truncated: true });
  const client = new QueryClient();
  const result = render(<QueryClientProvider client={client}><LocalFilePreview location={location} scope={scope} chinese={false} /></QueryClientProvider>);
  await screen.findByRole("button", { name: "Source" });
  expect(screen.getByText(/First 1 MB shown/)).toBeInTheDocument();
  expect(result.container.querySelector(".markdown-content")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Source" }));
  expect(result.container.querySelector("pre")).toBeInTheDocument();
});

it("renders local Markdown through the shared sanitizer", async () => {
  read.mockResolvedValue({ ...file, text: '# Safe heading\n\n<script>alert("unsafe")</script>\n\n<img src="x" onerror="alert(1)">\n\n[Unsafe](javascript:alert(1))' });
  const result = render(<QueryClientProvider client={new QueryClient()}><LocalFilePreview location={location} scope={scope} chinese={false} /></QueryClientProvider>);
  await screen.findByRole("heading", { name: "Safe heading" });
  expect(result.container.querySelector("script, [onerror], a[href^='javascript:']")).not.toBeInTheDocument();
});

it("limits Markdown rendering to the same first 5,000 lines as source", async () => {
  read.mockResolvedValue({ ...file, text: `${"line\n".repeat(5000)}# Outside preview` });
  render(<QueryClientProvider client={new QueryClient()}><LocalFilePreview location={location} scope={scope} chinese={false} /></QueryClientProvider>);
  await screen.findByRole("button", { name: "Source" });
  expect(screen.getByText(/First 5,000 lines shown/)).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Outside preview" })).not.toBeInTheDocument();
});

it.each([
  { name: "page.html", mimeType: "text/html", text: "<h1>HTML</h1>", element: "iframe" },
  { name: "image.png", mimeType: "image/png", url: "data:image/png;base64,AA==", element: "img" },
  { name: "document.pdf", mimeType: "application/pdf", url: "https://example.com/file.pdf", element: "iframe" },
  { name: "notes.txt", mimeType: "text/plain", text: "# Plain text", element: "pre" },
])("preserves $mimeType previews", async ({ element, ...preview }) => {
  read.mockResolvedValue({ ...file, ...preview });
  const result = render(<QueryClientProvider client={new QueryClient()}><LocalFilePreview location={{ ...location, name: preview.name, path: `/project/${preview.name}` }} scope={scope} chinese={false} /></QueryClientProvider>);
  await waitFor(() => expect(result.container.querySelector(element)).toBeInTheDocument());
  if (preview.mimeType === "text/html") {
    fireEvent.click(screen.getByRole("button", { name: "Source" }));
    expect(result.container.querySelector("pre")).toHaveTextContent("<h1>HTML</h1>");
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(result.container.querySelector("iframe")).toHaveAttribute("sandbox", "allow-scripts");
  } else expect(screen.queryByRole("button", { name: "Source" })).not.toBeInTheDocument();
});
