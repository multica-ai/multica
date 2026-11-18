import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockIntentNavigate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ agentDetail: (id: string) => `/acme/agents/${id}` }),
}));
vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
  useIntentNavigate: () => mockIntentNavigate,
}));

import { AgentRowActions } from "./agent-row-actions";

const agent = { id: "agent-1", name: "Lambda", archived_at: null } as Agent;

function openMenu(provider = "claude", archived = false, canManage = false) {
  renderWithI18n(
    <QueryClientProvider client={new QueryClient()}>
      <AgentRowActions
        agent={{ ...agent, archived_at: archived ? "2026-01-01" : null }}
        runtime={{ provider } as AgentRuntime}
        presence={null}
        canManage={canManage}
        duplicateHref="/acme/agents/new?duplicate=agent-1"
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Row actions" }));
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("AgentRowActions", () => {
  it.each([false, true])("hides Kagent lifecycle actions (archived: %s)", (archived) => {
    openMenu("kagent", archived, true);
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Restore" })).toBeNull();
    expect(screen.getByText("Kagent agents cannot be archived or restored.")).toBeInTheDocument();
  });

  it.each([false, true])("keeps other providers' lifecycle actions (archived: %s)", (archived) => {
    openMenu("claude", archived, true);
    expect(screen.getByRole("menuitem", { name: archived ? "Restore" : "Archive" })).toBeInTheDocument();
  });

  it("opens the agent's settings in place", () => {
    openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Settings" }));
    expect(mockIntentNavigate).toHaveBeenCalledWith(
      "/acme/agents/agent-1?view=general",
      "push",
    );
  });
});
