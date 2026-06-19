// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enSettings from "../../../locales/en/settings.json";

// IntegrationsTab's job is to pick which copy sits beside the bind entry
// based on (configured / install_supported / role). The bind entry itself
// is the shared LarkAgentBindButton, exhaustively covered in
// lark-tab.test.tsx — here we stub it to a marker so the tests assert the
// branch selection, not the install flow.
type MemberRole = "owner" | "admin" | "member" | "guest";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as unknown[],
    configured: true,
    install_supported: true,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current };
    if (key.includes("installations")) return { data: installationsRef.current };
    return { data: undefined };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({
    queryKey: ["lark", "installations"],
    queryFn: vi.fn(),
  }),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("../../../settings/components/lark-tab", () => ({
  LarkAgentBindButton: ({ agentId }: { agentId: string }) => (
    <div data-testid="lark-bind-button" data-agent-id={agentId} />
  ),
}));

import { IntegrationsTab } from "./integrations-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, settings: enSettings },
};

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = {
    installations: [],
    configured: true,
    install_supported: true,
  };
}

describe("IntegrationsTab", () => {
  beforeEach(resetFixtures);

  it("renders the shared bind entry for an owner when Lark is configured and supported", () => {
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText("Lark")).toBeTruthy();
    const button = screen.getByTestId("lark-bind-button");
    expect(button.getAttribute("data-agent-id")).toBe("agent-1");
  });

  it("shows the coming-soon notice when the install transport is not wired", () => {
    installationsRef.current = {
      installations: [],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText(/installation coming soon/i)).toBeTruthy();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("shows the not-enabled notice when the deployment has no Lark key", () => {
    installationsRef.current = {
      installations: [],
      configured: false,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByText(/Lark integration not enabled/i)).toBeTruthy();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("points members at Settings instead of a dead button when they can't manage", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    renderTab(<IntegrationsTab agent={agent} />);
    expect(
      screen.getByText(/Only workspace owners and admins can bind a Lark Bot/i),
    ).toBeTruthy();
    expect(screen.queryByTestId("lark-bind-button")).toBeNull();
  });

  it("renders the bind entry (not coming-soon) when installs are unavailable but the agent is already bound", () => {
    // install_supported governs only NEW installs; an already-bound agent
    // must still surface its connected state instead of "coming soon"
    // (regression for the must-fix on MUL-2988).
    installationsRef.current = {
      installations: [{ agent_id: "agent-1", status: "active" }],
      configured: true,
      install_supported: false,
    };
    renderTab(<IntegrationsTab agent={agent} />);
    expect(screen.getByTestId("lark-bind-button")).toBeTruthy();
    expect(screen.queryByText(/installation coming soon/i)).toBeNull();
  });
});
