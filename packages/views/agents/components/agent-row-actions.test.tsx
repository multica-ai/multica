import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
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

function openMenu() {
  renderWithI18n(
    <QueryClientProvider client={new QueryClient()}>
      <AgentRowActions
        agent={agent}
        presence={null}
        canManage={false}
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
  it("opens the agent's settings in place", () => {
    openMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: "Settings" }));
    expect(mockIntentNavigate).toHaveBeenCalledWith(
      "/acme/agents/agent-1?view=general",
      "push",
    );
  });
});
