import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { OctoAgentBindButton, OctoTab } from "./octo-tab";

type MemberRole = "owner" | "admin" | "member" | "guest";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const octoListingRef = vi.hoisted(() => ({
  current: {
    installations: [] as Array<{ id: string; agent_id: string; status: string; bot_name: string; robot_id: string }>,
    configured: true,
  } as {
    installations: Array<{ id: string; agent_id: string; status: string; bot_name: string; robot_id: string }>;
    configured: boolean;
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("octo")) return { data: octoListingRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/octo", () => ({
  octoInstallationsOptions: () => ({ queryKey: ["octo", "ws", "installations"], queryFn: vi.fn() }),
  octoKeys: { installations: (wsId: string) => ["octo", wsId, "installations"] },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: { id: "user-1" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: { id: "user-1" } }) },
  ),
}));

vi.mock("@multica/core/api", () => ({
  api: { createOctoInstallation: vi.fn(), deleteOctoInstallation: vi.fn() },
  ApiError: class ApiError extends Error {},
}));

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function renderButton(props: { agentId?: string; onShow?: () => void } = {}) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    );
  }
  return render(
    <OctoAgentBindButton
      agentId={props.agentId ?? "agent-1"}
      agentName="My Agent"
      onShowConnectedDetails={props.onShow}
    />,
    { wrapper: Wrapper },
  );
}

describe("OctoAgentBindButton", () => {
  beforeEach(() => {
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
    octoListingRef.current = { installations: [], configured: true };
  });

  it("renders the Connect CTA for an admin on a configured deployment with no binding", () => {
    renderButton();
    expect(screen.getByTestId("octo-agent-bind")).toBeInTheDocument();
    expect(screen.getByText(enSettings.octo.bind_button)).toBeInTheDocument();
  });

  it("renders nothing for non-admin members", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = renderButton();
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when Octo is not configured on the deployment", () => {
    octoListingRef.current = { installations: [], configured: false };
    const { container } = renderButton();
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the connected status row when this agent already has an active install", () => {
    octoListingRef.current = {
      installations: [
        { id: "i1", agent_id: "agent-1", status: "active", bot_name: "Helper Bot", robot_id: "r1" },
      ],
      configured: true,
    };
    renderButton();
    expect(screen.getByTestId("octo-agent-connected")).toBeInTheDocument();
    expect(screen.queryByTestId("octo-agent-bind")).not.toBeInTheDocument();
  });

  it("does not treat a different agent's install as this agent's binding", () => {
    octoListingRef.current = {
      installations: [
        { id: "i1", agent_id: "other-agent", status: "active", bot_name: "X", robot_id: "r1" },
      ],
      configured: true,
    };
    renderButton({ agentId: "agent-1" });
    // The CTA shows because agent-1 itself is unbound.
    expect(screen.getByTestId("octo-agent-bind")).toBeInTheDocument();
    expect(screen.queryByTestId("octo-agent-connected")).not.toBeInTheDocument();
  });
});

function renderTab() {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    );
  }
  return render(<OctoTab />, { wrapper: Wrapper });
}

describe("OctoTab status rendering", () => {
  beforeEach(() => {
    membersRef.current = [{ user_id: "user-1", role: "owner" }];
  });

  it("localizes known statuses and falls back to the raw value for an unknown one", () => {
    octoListingRef.current = {
      configured: true,
      installations: [
        { id: "i1", agent_id: "a1", status: "active", bot_name: "Active Bot", robot_id: "r1" },
        { id: "i2", agent_id: "a2", status: "revoked", bot_name: "Revoked Bot", robot_id: "r2" },
        // A status the backend might add later must render raw, not crash.
        { id: "i3", agent_id: "a3", status: "suspended", bot_name: "Future Bot", robot_id: "r3" },
      ],
    };
    renderTab();
    expect(screen.getByText(new RegExp(enSettings.octo.status_active))).toBeInTheDocument();
    expect(screen.getByText(new RegExp(enSettings.octo.status_revoked))).toBeInTheDocument();
    // Unknown enum value: downgraded to its raw string.
    expect(screen.getByText(/suspended/)).toBeInTheDocument();
  });
});
