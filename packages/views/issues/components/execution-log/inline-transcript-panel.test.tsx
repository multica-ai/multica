import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { InlineTranscriptPanel } from "./inline-transcript-panel";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import enIssues from "../../../locales/en/issues.json";

const mockListTaskMessages = vi.hoisted(() => vi.fn());
const mockIsEmbedded = vi.hoisted(() => vi.fn());
const mockPostNavigate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { listTaskMessages: mockListTaskMessages },
}));

vi.mock("@multica/core/platform", () => ({
  isEmbeddedInCostrict: mockIsEmbedded,
  postCostrictNavigateToSession: mockPostNavigate,
}));

const TEST_RESOURCES = { en: { issues: enIssues } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  status: "running",
  created_at: new Date().toISOString(),
  started_at: new Date().toISOString(),
} as AgentTask;

const messages: TaskMessagePayload[] = [
  { task_id: "task-1", issue_id: "issue-1", seq: 1, type: "text", content: "Hello" },
];

describe("InlineTranscriptPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTaskMessages.mockResolvedValue(messages);
    mockIsEmbedded.mockReturnValue(false);
  });

  it("expands to show fetched messages", async () => {
    render(<InlineTranscriptPanel task={task} isLive defaultOpen={false} />, { wrapper: Wrapper });
    await userEvent.click(screen.getByRole("button", { name: /show transcript/i }));
    await waitFor(() => expect(screen.getByText("Hello")).toBeInTheDocument());
  });

  it("shows live indicator when running", async () => {
    render(<InlineTranscriptPanel task={task} isLive defaultOpen />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText("Live")).toBeInTheDocument());
  });

  describe("View in CoStrict link", () => {
    const linkedTask = {
      ...task,
      session_id: "sess-xyz",
      work_dir: "/home/user/proj",
    } as AgentTask;

    it("renders the link only when embedded with session_id and work_dir", async () => {
      mockIsEmbedded.mockReturnValue(true);
      render(<InlineTranscriptPanel task={linkedTask} />, { wrapper: Wrapper });
      await waitFor(() =>
        expect(screen.getByRole("button", { name: /view in costrict/i })).toBeInTheDocument(),
      );
    });

    it("hides the link when not embedded", async () => {
      mockIsEmbedded.mockReturnValue(false);
      render(<InlineTranscriptPanel task={linkedTask} />, { wrapper: Wrapper });
      // Let the embed effect settle.
      await waitFor(() => expect(screen.getByRole("button", { name: /show transcript/i })).toBeInTheDocument());
      expect(screen.queryByRole("button", { name: /view in costrict/i })).not.toBeInTheDocument();
    });

    it("hides the link when embedded but session_id is missing", async () => {
      mockIsEmbedded.mockReturnValue(true);
      const noSession = { ...task, work_dir: "/home/user/proj" } as AgentTask;
      render(<InlineTranscriptPanel task={noSession} />, { wrapper: Wrapper });
      await waitFor(() => expect(screen.getByRole("button", { name: /show transcript/i })).toBeInTheDocument());
      expect(screen.queryByRole("button", { name: /view in costrict/i })).not.toBeInTheDocument();
    });

    it("posts the navigate message with sessionId and workDir on click", async () => {
      mockIsEmbedded.mockReturnValue(true);
      render(<InlineTranscriptPanel task={linkedTask} />, { wrapper: Wrapper });
      const link = await screen.findByRole("button", { name: /view in costrict/i });
      await userEvent.click(link);
      expect(mockPostNavigate).toHaveBeenCalledWith({
        sessionId: "sess-xyz",
        workDir: "/home/user/proj",
      });
    });
  });
});
