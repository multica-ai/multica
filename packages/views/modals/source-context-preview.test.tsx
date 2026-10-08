import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import enIssues from "../locales/en/issues.json";
import { SourceContextPreviewCard } from "./source-context-preview";

describe("SourceContextPreviewCard", () => {

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
