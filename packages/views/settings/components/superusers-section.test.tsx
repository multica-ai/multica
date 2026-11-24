import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  policy: { enabled: false, grants: [] as Array<{ member_id: string; user_id: string; name: string; email: string }> },
  set: vi.fn(),
  chats: vi.fn(),
  messages: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaceSuperusers: () => Promise.resolve(state.policy),
    setWorkspaceSuperuser: (...args: unknown[]) => state.set(...args),
    listSuperuserChats: (...args: unknown[]) => state.chats(...args),
    listSuperuserChatMessages: (...args: unknown[]) => state.messages(...args),
  },
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "owner" } }),
}));

import { SuperusersSection } from "./superusers-section";

const members = [
  { id: "m-owner", user_id: "owner", workspace_id: "ws", role: "owner" as const, name: "Ada", email: "ada@example.test", avatar_url: null, created_at: "2025-01-01" },
  { id: "m-grace", user_id: "grace", workspace_id: "ws", role: "member" as const, name: "Grace", email: "grace@example.test", avatar_url: null, created_at: "2025-01-01" },
];

function renderSection() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={client}>
    <SuperusersSection wsId="ws" members={members} isOwner />
  </QueryClientProvider>);
}

describe("SuperusersSection", () => {
  beforeEach(() => {
    state.policy.enabled = false;
    state.policy.grants = [];
    state.set.mockReset();
    state.chats.mockReset();
    state.messages.mockReset();
  });

  it("shows the policy before granting and refreshes the named holder after an explicit grant", async () => {
    state.set.mockImplementation(async () => {
      state.policy.enabled = true;
      state.policy.grants = [{ member_id: "m-grace", user_id: "grace", name: "Grace", email: "grace@example.test" }];
    });
    renderSection();
    expect(await screen.findByText("Superuser access has not been enabled.")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Choose a member" }), { target: { value: "m-grace" } });
    fireEvent.click(screen.getByRole("button", { name: "Grant access" }));
    expect(state.set).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Grant full access" }));
    await waitFor(() => expect(state.set).toHaveBeenCalledWith("ws", "m-grace", true));
    expect(await screen.findByText(/grace@example.test/)).toBeInTheDocument();
  });

  it("lets a granted human search and inspect a transcript without edit controls", async () => {
    state.policy.enabled = true;
    state.policy.grants = [{ member_id: "m-owner", user_id: "owner", name: "Ada", email: "ada@example.test" }];
    state.chats.mockResolvedValue([{ id: "chat-1", creator_id: "grace", agent_id: "a", title: "Incident", status: "active", updated_at: "2025-01-01" }]);
    state.messages.mockResolvedValue([{ id: "msg-1", role: "user", content: "What happened?" }]);
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: /Incident/ }));
    expect(await screen.findByText("What happened?")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /delete|send|edit/i })).not.toBeInTheDocument();
  });
});
