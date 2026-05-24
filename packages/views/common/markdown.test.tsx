import * as React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { Markdown } from "./markdown";
import type { Attachment as AttachmentRecord } from "@multica/core/types";

// Capture the renderImage prop that Markdown passes to MarkdownBase.
const capturedRenderImage = vi.hoisted(
  () => ({
    current: null as
      | ((props: { src: string; alt: string }) => React.ReactNode)
      | null,
  }),
);

vi.mock("@multica/ui/markdown", () => ({
  Markdown: (props: {
    renderImage?: (p: { src: string; alt: string }) => React.ReactNode;
  }) => {
    capturedRenderImage.current = props.renderImage ?? null;
    return null;
  },
}));

// Controls the apiBaseUrl returned by the config store during each test.
const apiBaseUrlRef = vi.hoisted(() => ({ current: "" }));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (
    sel?: (s: { apiBaseUrl: string; cdnDomain: string }) => unknown,
  ) => {
    const state = { apiBaseUrl: apiBaseUrlRef.current, cdnDomain: "" };
    return sel ? sel(state) : state;
  },
}));

vi.mock("@multica/core/utils", () => ({
  stripTrailingSlash: (s: string | undefined) => {
    if (!s) return "";
    return s.endsWith("/") ? s.slice(0, -1) : s;
  },
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: () => null,
}));

vi.mock("../editor", () => ({
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  Attachment: ({
    attachment,
  }: {
    attachment:
      | { kind: "url"; url: string; filename: string }
      | { kind: "record"; attachment: AttachmentRecord };
  }) => {
    if (attachment.kind === "record") {
      return (
        <img
          data-testid="attachment-img"
          src={attachment.attachment.url}
          data-record-id={attachment.attachment.id}
          alt=""
        />
      );
    }
    return (
      <img
        data-testid="attachment-img"
        src={attachment.url}
        data-filename={attachment.filename}
        alt=""
      />
    );
  },
}));

describe("Markdown – renderImage URL resolution", () => {
  beforeEach(() => {
    capturedRenderImage.current = null;
    apiBaseUrlRef.current = "";
  });

  function captureRenderImage(
    apiBaseUrl: string,
    attachments?: AttachmentRecord[],
  ): (props: { src: string; alt: string }) => React.ReactNode {
    apiBaseUrlRef.current = apiBaseUrl;
    render(<Markdown attachments={attachments}>{"content"}</Markdown>);
    if (!capturedRenderImage.current) {
      throw new Error("renderImage was not captured");
    }
    return capturedRenderImage.current;
  }

  it("prepends apiBaseUrl to /uploads/ paths when no matching attachment record", () => {
    const renderImage = captureRenderImage("https://api.example.com");
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/image.png", alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "https://api.example.com/uploads/image.png",
    );
  });

  it("strips trailing slash from apiBaseUrl before prepending", () => {
    const renderImage = captureRenderImage("https://api.example.com/");
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/image.png", alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "https://api.example.com/uploads/image.png",
    );
  });

  it("leaves absolute http URLs unchanged", () => {
    const renderImage = captureRenderImage("https://api.example.com");
    const { getByTestId } = render(
      <>{renderImage({ src: "https://cdn.example.com/image.png", alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "https://cdn.example.com/image.png",
    );
  });

  it("leaves data: URLs unchanged", () => {
    const renderImage = captureRenderImage("https://api.example.com");
    const dataUrl = "data:image/png;base64,abc123";
    const { getByTestId } = render(
      <>{renderImage({ src: dataUrl, alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute("src", dataUrl);
  });

  it("does not rewrite protocol-relative URLs (//host/path)", () => {
    const renderImage = captureRenderImage("https://api.example.com");
    const { getByTestId } = render(
      <>{renderImage({ src: "//cdn.example.com/image.png", alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "//cdn.example.com/image.png",
    );
  });

  it("when apiBaseUrl is empty, relative paths are preserved as-is", () => {
    const renderImage = captureRenderImage("");
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/image.png", alt: "photo" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "/uploads/image.png",
    );
  });

  it("passes the alt text through as filename for url-only path", () => {
    const renderImage = captureRenderImage("https://api.example.com");
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/img.png", alt: "diagram" })}</>,
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "data-filename",
      "diagram",
    );
  });

  it("uses attachment record when src matches a known attachment URL", () => {
    const record: AttachmentRecord = {
      id: "att-1",
      url: "/uploads/image.png",
      filename: "image.png",
      content_type: "image/png",
      size_bytes: 1000,
      download_url: "/uploads/image.png",
      workspace_id: "ws-1",
      uploader_id: "u-1",
      uploader_type: "member",
      issue_id: null,
      comment_id: null,
      chat_message_id: null,
      chat_session_id: null,
      created_at: "2024-01-01T00:00:00Z",
    };
    const renderImage = captureRenderImage("https://api.example.com", [record]);
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/image.png", alt: "photo" })}</>,
    );
    // Record path: src is the original relative URL (not prefixed), record-id is set
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "data-record-id",
      "att-1",
    );
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "/uploads/image.png",
    );
  });

  it("falls back to url-only path when src does not match any attachment record", () => {
    const record: AttachmentRecord = {
      id: "att-1",
      url: "/uploads/other.png",
      filename: "other.png",
      content_type: "image/png",
      size_bytes: 1000,
      download_url: "/uploads/other.png",
      workspace_id: "ws-1",
      uploader_id: "u-1",
      uploader_type: "member",
      issue_id: null,
      comment_id: null,
      chat_message_id: null,
      chat_session_id: null,
      created_at: "2024-01-01T00:00:00Z",
    };
    const renderImage = captureRenderImage("https://api.example.com", [record]);
    const { getByTestId } = render(
      <>{renderImage({ src: "/uploads/different.png", alt: "photo" })}</>,
    );
    // Falls back to url path with apiBaseUrl prefix
    expect(getByTestId("attachment-img")).toHaveAttribute(
      "src",
      "https://api.example.com/uploads/different.png",
    );
  });
});
