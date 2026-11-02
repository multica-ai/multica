// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { ApiError } from "@multica/core/api/client";
import type { KnowledgeBases } from "@multica/core/knowledge";
import { renderWithI18n } from "../test/i18n";
import { SaveIssueToKnowledgeModal } from "./save-issue-to-knowledge";

vi.mock("@multica/core/api", () => ({
  api: {
    listKnowledgeBases: vi.fn(),
    createKnowledgeDocument: vi.fn(),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const ready: KnowledgeBases = {
  offered: true,
  state: "ready",
  knowledge_bases: [
    { id: "b1", key: "market", name: "Market", visibility: "workspace", position: 1 },
    { id: "b2", key: "engineering", name: "Engineering", visibility: "workspace", position: 3 },
  ],
};

const issue = {
  issueId: "issue-1",
  title: "Login fails on Safari",
  description: "Repro: open the login page in Safari 17.",
};

function mount(data: Record<string, unknown> = issue) {
  const onClose = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <SaveIssueToKnowledgeModal onClose={onClose} data={data} />
    </QueryClientProvider>,
  );
  return { onClose };
}

const saveButton = () => screen.getByRole("button", { name: "Save" });

describe("SaveIssueToKnowledgeModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(ready);
    vi.mocked(api.createKnowledgeDocument).mockResolvedValue({} as never);
  });

  it("saves the issue as a published document in the engineering base by default", async () => {
    const { onClose } = mount();

    await waitFor(() => expect(saveButton()).toBeEnabled());
    fireEvent.click(saveButton());

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(api.createKnowledgeDocument).toHaveBeenCalledWith(
      expect.objectContaining({
        kb: "engineering",
        title: "Login fails on Safari",
        content: "Repro: open the login page in Safari 17.",
        source_type: "issue",
        source_id: "issue-1",
        publish: true,
        idempotency_key: expect.stringMatching(/^issue:issue-1:/),
      }),
    );
    expect(toast.success).toHaveBeenCalledWith("Saved to Engineering");
  });

  it("keeps Save disabled and says why when the issue has no description", async () => {
    mount({ ...issue, description: "   " });

    await waitFor(() => expect(screen.getByText("This issue has no description to save.")).toBeInTheDocument());
    await waitFor(() => expect(saveButton()).toBeDisabled());
    expect(api.createKnowledgeDocument).not.toHaveBeenCalled();
  });

  it("reports an offline knowledge service rather than a generic failure", async () => {
    vi.mocked(api.createKnowledgeDocument).mockRejectedValue(
      new ApiError("offline", 503, "Service Unavailable", { state: "offline" }),
    );
    const { onClose } = mount();

    await waitFor(() => expect(saveButton()).toBeEnabled());
    fireEvent.click(saveButton());

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("The knowledge service is offline"));
    expect(onClose).not.toHaveBeenCalled();
  });
});
