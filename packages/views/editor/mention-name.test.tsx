import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "agents"],
    queryFn: async () => [],
  }),
  memberListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "members"],
    queryFn: async () => [],
  }),
  squadListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "squads"],
    queryFn: async () => [],
  }),
}));

import { EntityMentionName } from "./mention-name";

function renderWithCache(ui: ReactNode, seed: (qc: QueryClient) => void = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  seed(qc);
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("EntityMentionName", () => {
  it("renders the cached agent name when the UUID resolves", () => {
    renderWithCache(
      <EntityMentionName type="agent" id="agent-real" fallbackLabel="@FakeLabel" />,
      (qc) =>
        qc.setQueryData(["workspaces", "ws-test", "agents"], [
          { id: "agent-real", name: "RealAgent" },
        ]),
    );

    expect(screen.getByText("@RealAgent")).toBeInTheDocument();
    expect(screen.queryByText("@FakeLabel")).not.toBeInTheDocument();
  });

  it("falls back to the markdown label when the agent is not cached", () => {
    renderWithCache(
      <EntityMentionName type="agent" id="ghost-agent" fallbackLabel="@Ghost" />,
    );

    expect(screen.getByText("@Ghost")).toBeInTheDocument();
  });

  it("looks up members by user_id rather than member row id", () => {
    renderWithCache(
      <EntityMentionName type="member" id="user-7" fallbackLabel="@Stale" />,
      (qc) =>
        qc.setQueryData(["workspaces", "ws-test", "members"], [
          { id: "member-row-1", user_id: "user-7", name: "Alice" },
        ]),
    );

    expect(screen.getByText("@Alice")).toBeInTheDocument();
  });

  it("renders the cached squad name when the UUID resolves", () => {
    renderWithCache(
      <EntityMentionName type="squad" id="squad-1" fallbackLabel="@OldName" />,
      (qc) =>
        qc.setQueryData(["workspaces", "ws-test", "squads"], [
          { id: "squad-1", name: "RealSquad" },
        ]),
    );

    expect(screen.getByText("@RealSquad")).toBeInTheDocument();
  });

  it("normalizes a fallback label that already starts with @", () => {
    renderWithCache(
      <EntityMentionName type="agent" id="missing" fallbackLabel="@Foo" />,
    );

    // Should render exactly "@Foo", not "@@Foo".
    expect(screen.getByText("@Foo")).toBeInTheDocument();
  });

  it("adds an @ prefix when the fallback label is missing one", () => {
    renderWithCache(
      <EntityMentionName type="agent" id="missing" fallbackLabel="Bare" />,
    );

    expect(screen.getByText("@Bare")).toBeInTheDocument();
  });
});
