import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import enIssues from "../locales/en/issues.json";
import { SourceContextPreviewCard } from "./source-context-preview";

describe("SourceContextPreviewCard", () => {
  it("emphasizes a comment's left quote border on hover", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard preview={{
          source_issue: {
            id: "issue-1", identifier: "MUL-1", number: 1, title: "Source",
            description: null, created_at: "now", updated_at: "now", revision: 1,
            attachments: [],
          },
          comment_thread: [{
            id: "comment-1", parent_id: null, type: "comment", content: "Quoted context",
            author: { type: "member", id: "user-1", name: "Alice" },
            created_at: "now", updated_at: "now", revision: 1, attachments: [],
          }],
          anchor_comment_id: "comment-1",
          capture_token: "sha256:token",
          limits: { comment_count: 1, text_bytes: 14, attachment_count: 0, attachment_bytes: 0 },
        }} />
      </I18nProvider>,
    );

    expect(screen.getByText(
      "Context from MUL-1 · issue description + 1 comment",
    )).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Context from MUL-1/ }));
    expect(screen.getByText("Alice").closest("li")).toHaveClass(
      "transition-colors",
      "duration-150",
      "hover:border-l-foreground/50",
    );
    expect(screen.getByText("Alice").parentElement).toHaveClass(
      "flex",
      "gap-2",
      "text-muted-foreground",
    );
    expect(screen.getByText("Quoted context").closest("div.mt-1\\.5")).toBeInTheDocument();
    expect(screen.getByText("Alice").closest("ol")?.parentElement).toHaveClass("space-y-8");
    expect(screen.getByText("Alice").closest('[data-slot="source-context-content"]')).toBeInTheDocument();
    expect(screen.getByText("Source comment")).toHaveClass(
      "rounded",
      "bg-info/10",
      "px-1.5",
      "py-0.5",
      "text-info",
    );
  });

  it("pluralizes the preview comment count", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard preview={{
          source_issue: {
            id: "issue-1", identifier: "MUL-1", number: 1, title: "Source",
            description: null, created_at: "now", updated_at: "now", revision: 1,
            attachments: [],
          },
          comment_thread: ["comment-1", "comment-2"].map((id, index) => ({
            id, parent_id: index === 0 ? null : "comment-1", type: "comment",
            content: `Quoted context ${index + 1}`,
            author: { type: "member", id: `user-${index + 1}`, name: index === 0 ? "Alice" : "Bob" },
            created_at: "now", updated_at: "now", revision: 1, attachments: [],
          })),
          anchor_comment_id: "comment-2",
          capture_token: "sha256:token",
          limits: { comment_count: 2, text_bytes: 32, attachment_count: 0, attachment_bytes: 0 },
        }} />
      </I18nProvider>,
    );

    expect(screen.getByText(
      "Context from MUL-1 · issue description + 2 comments",
    )).toBeInTheDocument();
  });

  it("does not render separate attachment lists in quoted source content", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard preview={{
          source_issue: {
            id: "issue-1", identifier: "MUL-1", number: 1, title: "Source",
            description: "Issue description", created_at: "now", updated_at: "now", revision: 1,
            attachments: [{
              id: "issue-attachment", owner_type: "issue", owner_id: "issue-1",
              filename: "issue-attachment.txt", content_type: "text/plain", size_bytes: 12,
              created_at: "now",
            }],
          },
          comment_thread: [{
            id: "comment-1", parent_id: null, type: "comment", content: "Quoted context",
            author: { type: "member", id: "user-1", name: "Alice" },
            created_at: "now", updated_at: "now", revision: 1,
            attachments: [{
              id: "comment-attachment", owner_type: "comment", owner_id: "comment-1",
              filename: "comment-attachment.txt", content_type: "text/plain", size_bytes: 12,
              created_at: "now",
            }],
          }],
          anchor_comment_id: "comment-1",
          capture_token: "sha256:token",
          limits: { comment_count: 1, text_bytes: 14, attachment_count: 2, attachment_bytes: 24 },
        }} />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: /Context from MUL-1/ }));
    expect(screen.getByText("Issue description")).toBeInTheDocument();
    expect(screen.getByText("Quoted context")).toBeInTheDocument();
    expect(screen.queryByText("issue-attachment.txt")).not.toBeInTheDocument();
    expect(screen.queryByText("comment-attachment.txt")).not.toBeInTheDocument();
  });

  it("stays visible when collapsed and scrolls internally when parent-constrained", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard
          constrainToParent
          preview={{
            source_issue: {
              id: "issue-1", identifier: "MUL-1", number: 1, title: "Source",
              description: null, created_at: "now", updated_at: "now", revision: 1,
              attachments: [],
            },
            comment_thread: [{
              id: "comment-1", parent_id: null, type: "comment", content: "Quoted context",
              author: { type: "member", id: "user-1", name: "Alice" },
              created_at: "now", updated_at: "now", revision: 1, attachments: [],
            }],
            anchor_comment_id: "comment-1",
            capture_token: "sha256:token",
            limits: { comment_count: 1, text_bytes: 14, attachment_count: 0, attachment_bytes: 0 },
          }}
        />
      </I18nProvider>,
    );

    const header = screen.getByRole("button", { name: /Context from MUL-1/ });
    const card = document.querySelector('[data-slot="source-context-preview"]');
    expect(card).toHaveClass("shrink-0");

    fireEvent.click(header);
    expect(card).toHaveClass("flex", "h-[53%]", "min-h-0", "shrink-0", "flex-col");
    expect(card).not.toHaveClass("max-h-[65%]", "shrink");
    expect(header).toHaveClass("shrink-0");
    const body = document.querySelector('[data-slot="source-context-preview-body"]');
    expect(body).toHaveClass("min-h-0", "flex-1", "overflow-y-auto", "overscroll-contain");
    expect(body).not.toHaveClass("max-h-72");
  });

  it("explains every exceeded source-context limit from the structured response", () => {
    const error = new ApiError("too large", 422, "Unprocessable Entity", {
      code: "source_context_too_large",
      limits: {
        comment_count: 257,
        text_bytes: 1024 * 1024 + 1,
        attachment_count: 101,
        attachment_bytes: 500 * 1024 * 1024 + 1,
      },
    });
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard failed error={error} />
      </I18nProvider>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("257 comments");
    expect(screen.getByRole("alert")).toHaveTextContent("1 MiB");
    expect(screen.getByRole("alert")).toHaveTextContent("101 attachments");
    expect(screen.getByRole("alert")).toHaveTextContent("500 MiB");
  });

  it.each([
    ["anchor_comment_deleted", "The source comment was deleted"],
    ["source_issue_deleted", "The source issue was deleted"],
  ])("explains terminal deletion error %s without offering refresh", (code, message) => {
    const onRetry = vi.fn();
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard
          failed
          error={new ApiError("deleted", 409, "Conflict", { code })}
          onRetry={onRetry}
        />
      </I18nProvider>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(message);
    expect(screen.queryByRole("button", { name: "Refresh" })).not.toBeInTheDocument();
  });

  it("keeps refresh available for a potentially recoverable preview failure", () => {
    const onRetry = vi.fn();
    render(
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <SourceContextPreviewCard
          failed
          error={new ApiError("temporary failure", 503, "Service Unavailable")}
          onRetry={onRetry}
        />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
