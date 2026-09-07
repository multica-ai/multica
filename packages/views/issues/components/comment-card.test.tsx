import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import enIssues from "../../locales/en/issues.json";
import type { TimelineEntry } from "@multica/core/types";
import { toast } from "sonner";

const { runFollowUpMock, followUpState } = vi.hoisted(() => ({
  runFollowUpMock: vi.fn(),
  followUpState: { isPending: false, variables: undefined as { actionId: string } | undefined },
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), info: vi.fn(), error: vi.fn() } }));

vi.mock("@multica/core/issues", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues")>();
  return {
    ...actual,
    useRunIssueCommentFollowUp: () => ({
      mutateAsync: runFollowUpMock,
      ...followUpState,
    }),
  };
});

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Builder" }),
}));

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: vi.fn(),
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

// HtmlAttachmentPreview (kind="html" dispatch from AttachmentBlock) reads
// useNavigation() + useWorkspaceSlug() for the Open-in-new-tab button.
// Mock both so the standalone-attachment-routes-to-iframe test does not
// need the surrounding NavigationProvider / WorkspaceSlugProvider tree.
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
  };
});

import { AttachmentList, CommentFollowUps } from "./comment-card";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function renderFollowUps(ui: ReactElement) {
  return renderWithQuery(
    <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
      <TooltipProvider>{ui}</TooltipProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  followUpState.isPending = false;
  followUpState.variables = undefined;
});
afterEach(() => vi.restoreAllMocks());

describe("AttachmentList — standalone HTML attachment routes through AttachmentBlock", () => {
  // Regression pin for comment-card.tsx:152. This is the entry point
  // MUL-2330 originally regressed on: standalone HTML attachments (not
  // referenced inline in the markdown body) MUST render through
  // <AttachmentBlock> so the html+attachmentId dispatch fires. Reverting to
  // <AttachmentCard> here re-introduces the "report.html shows as a bare
  // file card row instead of the rendered chart" bug.
  it("renders an iframe (no file-card chrome) for a standalone HTML attachment", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    const attachment = {
      id: "att-1",
      url: "/uploads/report.html",
      filename: "report.html",
      content_type: "text/html",
      size_bytes: 0,
    } as any;

    renderWithQuery(<AttachmentList attachments={[attachment]} content="" />);

    const frame = await waitFor(() => {
      const f = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(f).toBeTruthy();
      return f!;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>chart</p>");
    // AttachmentCard chrome would render the filename as visible <p> text;
    // HtmlAttachmentPreview replaces the row entirely.
    expect(screen.queryByText("report.html")).toBeNull();
  });
});

describe("AttachmentList — inline attachment filtering", () => {
  it("does not render a bottom attachment row when the body already has the stable file-card URL", () => {
    const id = "11111111-2222-3333-4444-555555555555";
    const href = `/api/attachments/${id}/download`;
    const attachment = {
      id,
      url: "/uploads/report.pdf",
      filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList
        attachments={[attachment]}
        content={`!file[report.pdf](${href})`}
      />,
    );

    expect(screen.queryByText("report.pdf")).toBeNull();
    expect(container.firstChild).toBeNull();
  });

  it("does not render a bottom attachment row when the body already has the response download_url", () => {
    const href = "https://cdn.example.test/report.pdf?Signature=stale";
    const attachment = {
      id: "11111111-2222-3333-4444-555555555555",
      url: "/uploads/report.pdf",
      download_url: "https://cdn.example.test/report.pdf?Signature=fresh",
      filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList
        attachments={[attachment]}
        content={`!file[report.pdf](${href})`}
      />,
    );

    expect(screen.queryByText("report.pdf")).toBeNull();
    expect(container.firstChild).toBeNull();
  });
});

describe("CommentFollowUps", () => {
  const entry: TimelineEntry = {
    type: "comment", id: "comment-1", actor_type: "agent", actor_id: "agent-1",
    created_at: "2026-08-31T00:00:00Z", content: "The first pass is ready.",
    suggested_follow_ups: [
      { id: "continue-1", label: "Continue", prompt: "Continue the implementation.", primary: true },
      { id: "review-1", label: "Review", prompt: "Review the current result." },
    ],
  };

  it("renders two safe actions and runs the selected server action id", async () => {
    runFollowUpMock.mockResolvedValueOnce({
      trigger_outcomes: [{ status: "queued" }],
    });
    renderFollowUps(<CommentFollowUps issueId="issue-1" entry={entry} active />);

    expect(screen.getByRole("button", { name: /Continue/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Review" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));

    await waitFor(() => {
      expect(runFollowUpMock).toHaveBeenCalledWith({
        commentId: "comment-1",
        actionId: "continue-1",
      });
    });
  });

  it("hides stale actions when the comment is not the active thread tail", () => {
    renderFollowUps(<CommentFollowUps issueId="issue-1" entry={entry} active={false} />);

    expect(screen.queryByRole("button", { name: "Continue" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Review" })).toBeNull();
  });

  it("disables all actions while the selected action is pending", () => {
    followUpState.isPending = true;
    followUpState.variables = { actionId: "continue-1" };
    renderFollowUps(<CommentFollowUps issueId="issue-1" entry={entry} active />);
    for (const button of screen.getAllByRole("button")) {
      expect(button).toBeDisabled();
      fireEvent.click(button);
    }
    expect(runFollowUpMock).not.toHaveBeenCalled();
  });

  it.each([
    ["queued", "success", enIssues.detail.quick_action_queued],
    ["coalesced", "info", enIssues.detail.quick_action_coalesced],
    ["deferred", "info", enIssues.detail.quick_action_deferred],
    ["blocked", "error", enIssues.detail.quick_action_blocked],
    ["future-status", "info", enIssues.detail.quick_action_posted],
    [undefined, "info", enIssues.detail.quick_action_posted],
  ] as const)("reports %s dispatch honestly", async (status, kind, message) => {
    runFollowUpMock.mockResolvedValueOnce({ trigger_outcomes: status ? [{ status }] : [] });
    renderFollowUps(<CommentFollowUps issueId="issue-1" entry={entry} active />);
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));
    await waitFor(() => expect(toast[kind]).toHaveBeenCalledWith(message.replace("{{name}}", "Builder")));
    if (kind !== "success") expect(toast.success).not.toHaveBeenCalled();
  });

  it("reports a stale-action error without a success notification", async () => {
    runFollowUpMock.mockRejectedValueOnce(new Error("a newer reply already exists in this thread"));
    renderFollowUps(<CommentFollowUps issueId="issue-1" entry={entry} active />);
    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("a newer reply already exists in this thread"));
    expect(toast.success).not.toHaveBeenCalled();
  });
});
