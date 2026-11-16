// @vitest-environment jsdom

import type { ReactNode } from "react";
import { cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EMPTY_AGENT_DRAFT } from "@multica/core/agents";
import { renderWithI18n } from "../../test/i18n";
import { ManualCreateAgentPage } from "./manual-create-agent-page";

const nav = vi.hoisted(() => ({
  pathname: "/acme/agents/new/manual",
  searchParams: new URLSearchParams(),
  push: vi.fn(),
  replace: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    newAgent: () => "/acme/agents/new",
    newAgentManual: () => "/acme/agents/new/manual",
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => [] }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => nav,
  useBackOrReplace: () => vi.fn(),
}));
vi.mock("./use-create-agent-form", () => ({
  useCreateAgentForm: () => ({
    draft: { ...EMPTY_AGENT_DRAFT, runtimeId: "rt-1" },
    setDraft: vi.fn(),
    runtimes: [],
    runtimesLoading: false,
    runtimesSettled: true,
    usableRuntimes: [],
    selectedRuntime: null,
    members: [],
    currentUserId: "user-1",
    draftReady: true,
  }),
}));
vi.mock("./use-create-agent-submit", () => ({
  useCreateAgentSubmit: () => ({
    creating: false,
    nameError: null,
    formError: null,
    clearNameError: vi.fn(),
    create: vi.fn(),
  }),
}));
vi.mock("./use-duplicate-draft-seed", () => ({ useDuplicateDraftSeed: () => {} }));
vi.mock("./use-manual-draft-sync", () => ({
  useManualDraftSync: () => {},
  clearManualDraftForOwner: vi.fn(),
}));
vi.mock("./agent-configuration-panel", () => ({ AgentConfigurationPanel: () => null }));
vi.mock("./create-agent-footer", () => ({ CreateAgentFooter: () => null }));
vi.mock("./create-shell", () => ({
  AgentCreateShell: ({ children }: { children: ReactNode }) => <>{children}</>,
  AgentCreateChip: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const page = () => (
    <QueryClientProvider client={qc}>
      <ManualCreateAgentPage />
    </QueryClientProvider>
  );
  const view = renderWithI18n(page());
  return { refresh: () => view.rerender(page()) };
}

describe("ManualCreateAgentPage runtime guard", () => {
  beforeEach(() => {
    nav.pathname = "/acme/agents/new/manual";
    nav.searchParams = new URLSearchParams();
    nav.replace.mockClear();
  });
  afterEach(cleanup);

  it("sends a visit without a runtime back to step 1", () => {
    mount();
    expect(nav.replace).toHaveBeenCalledWith("/acme/agents/new");
  });

  it("stays put when a runtime is chosen", () => {
    nav.searchParams = new URLSearchParams("runtime=rt-1");
    mount();
    expect(nav.replace).not.toHaveBeenCalled();
  });

  // Desktop: creating replaces the tab URL with the done page while this
  // route is still rendered.
  it("does not redirect once the location has moved to the done page", () => {
    nav.searchParams = new URLSearchParams("runtime=rt-1");
    const view = mount();
    nav.pathname = "/acme/agents/new/done/agent-1";
    nav.searchParams = new URLSearchParams();
    view.refresh();
    expect(nav.replace).not.toHaveBeenCalled();
  });
});
