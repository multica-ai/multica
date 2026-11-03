// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const decide = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { decideTaskToolConfirmation: decide },
}));

import { ToolConfirmationCard } from "./tool-confirmation-card";

function renderCard() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <ToolConfirmationCard
        issueId="issue-1"
        taskId="task-1"
        confirmation={{
          call_id: "call-1",
          tool_name: "deploy",
          mcp_server_name: "ops",
          input: { env: "prod" },
          status: "pending",
          created_at: "2026-06-08T08:00:00Z",
        }}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  decide.mockReset();
});

describe("ToolConfirmationCard", () => {
  it("allows the call", async () => {
    decide.mockResolvedValue(undefined);
    renderCard();
    expect(screen.getByText("ops/deploy needs approval")).toBeTruthy();
    expect(screen.getByText(/"env": "prod"/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    await waitFor(() =>
      expect(decide).toHaveBeenCalledWith("issue-1", "task-1", "call-1", {
        decision: "allow",
        message: undefined,
      }),
    );
  });

  it("denies the call with a reason", async () => {
    decide.mockResolvedValue(undefined);
    renderCard();
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    fireEvent.change(screen.getByLabelText("Reason for the agent (optional)"), {
      target: { value: " too risky " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() =>
      expect(decide).toHaveBeenCalledWith("issue-1", "task-1", "call-1", {
        decision: "deny",
        message: "too risky",
      }),
    );
  });
});
