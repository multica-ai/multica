// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const install = vi.hoisted(() => vi.fn());
const mintPairingCode = vi.hoisted(() => vi.fn());
const unbindCurrentUser = vi.hoisted(() => vi.fn());
const revokeInstallation = vi.hoisted(() => vi.fn());
const resetInstallMutation = vi.hoisted(() => vi.fn());
const resetPairingMutation = vi.hoisted(() => vi.fn());
const refetchInstallations = vi.hoisted(() => vi.fn());

const queriesRef = vi.hoisted(() => ({
  installations: {
    installations: [] as Array<Record<string, unknown>>,
    configured: true,
    pairingSupported: true as boolean | undefined,
  },
  agents: [
    {
      id: "agent-owned",
      name: "Owned Agent",
      archivedAt: null,
      canManage: true,
      canInvoke: true,
    },
    {
      id: "agent-other",
      name: "Other Agent",
      archivedAt: null,
      canManage: false,
      canInvoke: true,
    },
  ] as Array<Record<string, unknown>>,
  members: [{ user_id: "user-1", role: "member" }],
}));

const mutationStateRef = vi.hoisted(() => ({
  installPending: false,
  pairingPending: false,
  unbindPending: false,
  revokePending: false,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: unknown[] }) => {
    const key = JSON.stringify(options.queryKey);
    if (key.includes("qianwen")) {
      return {
        data: queriesRef.installations,
        isLoading: false,
        isFetching: false,
        refetch: refetchInstallations,
      };
    }
    if (key.includes("agents")) {
      return { data: queriesRef.agents, isLoading: false };
    }
    if (key.includes("members")) {
      return { data: queriesRef.members, isLoading: false };
    }
    return { data: undefined, isLoading: false };
  },
  queryOptions: <T,>(options: T) => options,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (selector?: (state: { user: { id: string } }) => unknown) => {
      const state = { user: { id: "user-1" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/qianwen", () => ({
  qianwenAgentListOptions: () => ({ queryKey: ["agents"] }),
  qianwenInstallationsOptions: () => ({ queryKey: ["qianwen", "installations"] }),
  useInstallQianwenPersonal: () => ({
    mutateAsync: install,
    isPending: mutationStateRef.installPending,
    reset: resetInstallMutation,
  }),
  useMintQianwenPairingCode: () => ({
    mutateAsync: mintPairingCode,
    isPending: mutationStateRef.pairingPending,
    reset: resetPairingMutation,
  }),
  useUnbindQianwenCurrentUser: () => ({
    mutateAsync: unbindCurrentUser,
    isPending: mutationStateRef.unbindPending,
  }),
  useRevokeQianwenInstallation: () => ({
    mutateAsync: revokeInstallation,
    isPending: mutationStateRef.revokePending,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { QianwenTab } from "./qianwen-tab";
import { toast } from "sonner";

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <QianwenTab />
    </I18nProvider>,
  );
}

afterEach(cleanup);

describe("QianwenTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutationStateRef.installPending = false;
    mutationStateRef.pairingPending = false;
    mutationStateRef.unbindPending = false;
    mutationStateRef.revokePending = false;
    queriesRef.installations = {
      installations: [],
      configured: true,
      pairingSupported: true,
    };
    queriesRef.agents = [
      {
        id: "agent-owned",
        name: "Owned Agent",
        archivedAt: null,
        canManage: true,
        canInvoke: true,
      },
      {
        id: "agent-other",
        name: "Other Agent",
        archivedAt: null,
        canManage: false,
        canInvoke: true,
      },
    ];
    queriesRef.members = [{ user_id: "user-1", role: "member" }];
    install.mockResolvedValue({
      id: "installation-1",
      agentId: "agent-owned",
      connectionId: "qwc_connection",
      mode: "personal_polling",
      status: "active",
      currentUserBound: false,
      accessToken: "qws_secret_once",
      tokenVisibleOnce: true,
      submitPath: "/api/channels/qianwen/qwc_connection/requests",
      statusPathPattern:
        "/api/channels/qianwen/qwc_connection/requests/{request_id}",
    });
    mintPairingCode.mockResolvedValue({
      pairingCode: "01234567",
      expiresAt: "2026-08-15T03:10:00Z",
      codeVisibleOnce: true,
    });
  });

  it("installs only a manageable Agent and keeps the one-time qws open until explicit acknowledgement", async () => {
    const user = userEvent.setup();
    renderTab();

    const agentSelect = screen.getByRole("combobox", { name: "Agent" });
    expect(agentSelect).toHaveTextContent("Owned Agent");
    expect(screen.queryByText("Other Agent")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Create connection" }));

    expect(install).toHaveBeenCalledWith({ agentId: "agent-owned" });
    expect(resetInstallMutation).toHaveBeenCalledOnce();
    expect(await screen.findByText("qws_secret_once")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();

    await user.keyboard("{Escape}");
    expect(screen.getByText("qws_secret_once")).toBeInTheDocument();
    fireEvent.pointerDown(document.body);
    fireEvent.click(document.body);
    expect(screen.getByText("qws_secret_once")).toBeInTheDocument();

    const acknowledge = screen.getByRole("checkbox", {
      name: "I saved this qws access token securely",
    });
    const savedButton = screen.getByRole("button", { name: "I've saved it" });
    expect(savedButton).toBeDisabled();
    await user.click(acknowledge);
    expect(savedButton).toBeEnabled();
    await user.click(savedButton);
    expect(screen.queryByText("qws_secret_once")).toBeNull();
  });

  it("shows connection lifecycle separately from caller-relative identity state", () => {
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-linked",
          agentId: "agent-owned",
          connectionId: "qwc_linked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: true,
        },
        {
          id: "installation-unlinked",
          agentId: "agent-other",
          connectionId: "qwc_unlinked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
        {
          id: "installation-unknown",
          agentId: "agent-owned",
          connectionId: "qwc_unknown",
          mode: "personal_polling",
          status: "revoked",
        },
      ],
    };

    renderTab();

    const linked = screen.getByRole("group", { name: "qwc_linked" });
    expect(within(linked).getByText("Connection active")).toBeInTheDocument();
    expect(within(linked).getByText("Identity linked")).toBeInTheDocument();

    const unlinked = screen.getByRole("group", { name: "qwc_unlinked" });
    expect(within(unlinked).getByText("Connection active")).toBeInTheDocument();
    expect(within(unlinked).getByText("Identity not linked")).toBeInTheDocument();

    const unknown = screen.getByRole("group", { name: "qwc_unknown" });
    expect(within(unknown).getByText("Connection revoked")).toBeInTheDocument();
    expect(within(unknown).getByText("Identity status unknown")).toBeInTheDocument();
    expect(screen.queryByText(/online/i)).toBeNull();
    expect(screen.queryByText(/published/i)).toBeNull();
  });

  it("does not treat an unknown caller-relative identity state as unlinked", () => {
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-unknown-active",
          agentId: "agent-owned",
          connectionId: "qwc_unknown_active",
          mode: "personal_polling",
          status: "active",
        },
      ],
    };

    renderTab();

    const card = screen.getByRole("group", { name: "qwc_unknown_active" });
    expect(within(card).getByText("Identity status unknown")).toBeInTheDocument();
    expect(
      within(card).queryByRole("button", { name: "Generate pairing code" }),
    ).toBeNull();
  });

  it("disables installation and pairing unless the backend explicitly supports pairing", () => {
    queriesRef.installations = {
      configured: true,
      pairingSupported: undefined,
      installations: [
        {
          id: "installation-unlinked",
          agentId: "agent-owned",
          connectionId: "qwc_unlinked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
      ],
    };

    renderTab();

    expect(screen.getByRole("combobox", { name: "Agent" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Create connection" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Generate pairing code" }),
    ).toBeDisabled();
    expect(
      screen.getByText(
        "Pairing is not available on this server. Existing connections remain visible and can still be revoked.",
      ),
    ).toBeInTheDocument();
  });

  it("keeps a leading-zero pairing code as text and explains the ten-minute expiry", async () => {
    const user = userEvent.setup();
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-unlinked",
          agentId: "agent-owned",
          connectionId: "qwc_unlinked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
      ],
    };

    renderTab();
    await user.click(screen.getByRole("button", { name: "Generate pairing code" }));

    expect(mintPairingCode).toHaveBeenCalledWith({
      installationId: "installation-unlinked",
    });
    expect(resetPairingMutation).toHaveBeenCalledOnce();
    expect(await screen.findByText("01234567")).toBeInTheDocument();
    expect(screen.getByText("This code expires in 10 minutes.")).toBeInTheDocument();
  });

  it("keeps unlinking the current identity separate from revoking the connection", async () => {
    const user = userEvent.setup();
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-linked",
          agentId: "agent-owned",
          connectionId: "qwc_linked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: true,
        },
      ],
    };
    unbindCurrentUser.mockResolvedValue(undefined);
    revokeInstallation.mockResolvedValue(undefined);

    renderTab();

    await user.click(screen.getByRole("button", { name: "Unlink my identity" }));
    expect(unbindCurrentUser).toHaveBeenCalledWith({
      installationId: "installation-linked",
    });
    expect(revokeInstallation).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Revoke connection" }));
    const confirmation = screen.getByRole("alertdialog");
    expect(
      within(confirmation).getByText(
        "This disables the Multica connection for everyone using it. It does not only unlink your identity.",
      ),
    ).toBeInTheDocument();
    await user.click(
      within(confirmation).getByRole("button", { name: "Revoke connection" }),
    );
    expect(revokeInstallation).toHaveBeenCalledWith({
      installationId: "installation-linked",
    });
  });

  it("keeps a redacted linked identity unlinkable without exposing Agent management", async () => {
    const user = userEvent.setup();
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-redacted-linked",
          agentId: "",
          connectionId: "",
          mode: "",
          status: "active",
          currentUserBound: true,
        },
      ],
    };
    unbindCurrentUser.mockResolvedValue(undefined);

    renderTab();

    const linkedStatus = screen.getByText("Identity linked");
    const redactedRow = linkedStatus.closest<HTMLElement>('[role="group"]');
    expect(redactedRow).not.toBeNull();
    const row = within(redactedRow as HTMLElement);
    expect(row.queryByText("Owned Agent")).toBeNull();
    expect(row.queryByText("Other Agent")).toBeNull();
    expect(row.queryByText(/qwc_/)).toBeNull();
    expect(row.queryByRole("button", { name: "Generate pairing code" })).toBeNull();
    expect(row.queryByRole("button", { name: "Revoke connection" })).toBeNull();

    await user.click(row.getByRole("button", { name: "Unlink my identity" }));
    expect(unbindCurrentUser).toHaveBeenCalledWith({
      installationId: "installation-redacted-linked",
    });
  });

  it("never exposes a raw mutation error in the UI notification", async () => {
    const user = userEvent.setup();
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-unlinked",
          agentId: "agent-owned",
          connectionId: "qwc_unlinked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
      ],
    };
    mintPairingCode.mockRejectedValue(
      new Error("raw response contained qws_do_not_expose"),
    );

    renderTab();
    await user.click(screen.getByRole("button", { name: "Generate pairing code" }));

    expect(toast.error).toHaveBeenCalledWith(
      "Could not update the Qianwen connection",
    );
    expect(JSON.stringify(vi.mocked(toast.error).mock.calls)).not.toContain(
      "qws_do_not_expose",
    );
    expect(screen.queryByText(/qws_do_not_expose/)).toBeNull();
  });

  it("does not offer pairing for an Agent the current user cannot invoke", () => {
    queriesRef.members = [{ user_id: "user-1", role: "admin" }];
    queriesRef.agents = [
      {
        id: "agent-private-other",
        name: "Private Agent",
        archivedAt: null,
        canManage: true,
        canInvoke: false,
      },
    ];
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-private",
          agentId: "agent-private-other",
          connectionId: "qwc_private",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
        {
          id: "installation-missing-agent",
          agentId: "agent-missing",
          connectionId: "qwc_missing",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
      ],
    };

    renderTab();

    for (const connectionId of ["qwc_private", "qwc_missing"]) {
      expect(
        within(screen.getByRole("group", { name: connectionId })).queryByRole(
          "button",
          { name: "Generate pairing code" },
        ),
      ).toBeNull();
    }
  });

  it("lets the user explicitly refresh caller-relative identity status", async () => {
    const user = userEvent.setup();
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-unlinked",
          agentId: "agent-owned",
          connectionId: "qwc_unlinked",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
      ],
    };
    refetchInstallations.mockResolvedValue(undefined);

    renderTab();
    await user.click(screen.getByRole("button", { name: "Refresh status" }));

    expect(refetchInstallations).toHaveBeenCalledOnce();
  });

  it("excludes an Agent with an active connection but allows a revoked one to reconnect", async () => {
    const user = userEvent.setup();
    queriesRef.members = [{ user_id: "user-1", role: "admin" }];
    queriesRef.agents = [
      {
        id: "agent-owned",
        name: "Owned Agent",
        archivedAt: null,
        canManage: true,
        canInvoke: true,
      },
      {
        id: "agent-other",
        name: "Other Agent",
        archivedAt: null,
        canManage: true,
        canInvoke: true,
      },
    ];
    queriesRef.installations = {
      configured: true,
      pairingSupported: true,
      installations: [
        {
          id: "installation-active",
          agentId: "agent-owned",
          connectionId: "qwc_active",
          mode: "personal_polling",
          status: "active",
          currentUserBound: false,
        },
        {
          id: "installation-revoked",
          agentId: "agent-other",
          connectionId: "qwc_revoked",
          mode: "personal_polling",
          status: "revoked",
          currentUserBound: false,
        },
      ],
    };

    renderTab();

    expect(screen.getByRole("combobox", { name: "Agent" })).toHaveTextContent(
      "Other Agent",
    );
    await user.click(screen.getByRole("button", { name: "Create connection" }));
    expect(install).toHaveBeenCalledWith({ agentId: "agent-other" });
  });
});
