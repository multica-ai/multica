// @vitest-environment jsdom

import { type ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// wecom-tab.test.tsx — the Settings → Integrations WeCom panel and the per-agent
// bind button. Mirrors dingtalk-tab.test.tsx. Covers the admin gate, the BYO
// install dialog + credential trimming, the connected badge, the not-enabled /
// empty states, and confirm-before-disconnect.
//
// Co-authored analysis: seacen (PR #5833 review).

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
const mockRegisterBYO = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("installations")) return { data: installationsRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getAgentName: (agentId: string) => `Agent ${agentId}`,
    getMemberName: () => "Unknown",
    getSquadName: () => "Unknown Squad",
    getActorName: () => "Unknown",
    getActorInitials: () => "??",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-id={actorId} />
  ),
}));

vi.mock("@multica/core/wecom", () => ({
  wecomInstallationsOptions: () => ({
    queryKey: ["wecom", "installations"],
    queryFn: vi.fn(),
  }),
  wecomKeys: { installations: (wsId: string) => ["wecom", "installations", wsId] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    registerWecomBYO: mockRegisterBYO,
    deleteWecomInstallation: mockDeleteInstallation,
  },
  // The real one digs the code out of an ApiError body; the fake reads it off
  // whatever the test threw, so a test can drive one branch of the switch.
  errorCode: (e: unknown) =>
    e && typeof e === "object" ? (e as { code?: string }).code : undefined,
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));

import { toast } from "sonner";
import { WecomAgentBindButton, WecomTab } from "./wecom-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

function renderUI(children: ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>,
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = { installations: [], configured: true, install_supported: true };
}

describe("WecomAgentBindButton", () => {
  beforeEach(resetFixtures);

  it("opens the BYO dialog and submits the trimmed bot id + secret", async () => {
    mockRegisterBYO.mockResolvedValue({ id: "i1", agent_id: "agent-1", status: "active" });
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "  aibot_xyz  ");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "  s3cr3t  ");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));
    await waitFor(() =>
      // Credential trimming: leading/trailing whitespace is stripped before send.
      expect(mockRegisterBYO).toHaveBeenCalledWith("workspace-1", "agent-1", {
        bot_id: "aibot_xyz",
        secret: "s3cr3t",
      }),
    );
  });

  // The whole point of the server sending a code: a zh-Hans admin gets the
  // Chinese sentence, not the English one the API happens to carry.
  it("renders the localized sentence for a coded failure, not the server's English", async () => {
    mockRegisterBYO.mockRejectedValue(
      Object.assign(new Error("this bot is already connected to another agent in this workspace"), {
        code: "wecom_bot_owned_by_same_workspace",
      }),
    );
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aibot_xyz");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cr3t");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(enSettings.wecom.byo_conflict_same_workspace),
    );
  });

  // The 503 branch: we could not reach WeCom, so nothing was verified and
  // nothing was changed. The admin must not read this as "your secret is
  // wrong" — that sends them to rotate one that was fine, and a rotated WeCom
  // secret cannot be recovered. Separate code, separate sentence.
  it("tells the admin their credentials were not changed when WeCom was unreachable", async () => {
    mockRegisterBYO.mockRejectedValue(
      Object.assign(new Error("could not reach WeCom to verify this bot"), {
        code: "wecom_credentials_unverifiable",
      }),
    );
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aibot_xyz");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cr3t");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(enSettings.wecom.byo_credentials_unverifiable),
    );
  });

  // WeCom refusing the pair is the one outcome entitled to blame the
  // credentials, and it must not reuse byo_rejected — that sentence means
  // "you left a field out".
  it("renders the credentials-rejected sentence, distinct from the missing-field one", async () => {
    mockRegisterBYO.mockRejectedValue(
      Object.assign(new Error("WeCom rejected this Bot ID and secret"), {
        code: "wecom_credentials_rejected",
      }),
    );
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aibot_xyz");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cr3t");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(enSettings.wecom.byo_credentials_rejected),
    );
    expect(toast.error).not.toHaveBeenCalledWith(enSettings.wecom.byo_rejected);
  });

  // A server that sends no code must still say something useful, so the
  // fallback cannot regress into the generic toast.
  it("falls back to the server's message when no code is attached", async () => {
    mockRegisterBYO.mockRejectedValue(new Error("something the server said"));
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aibot_xyz");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cr3t");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("something the server said"),
    );
  });

  // The bot's chat name reaches the server, and only when it was typed.
  //
  // WeCom delivers a group @-mention as literal text with no structured
  // mention list, so a name containing a space ("Multica Bot") swallows the
  // slash command typed after it. The name is the only way to tell where the
  // mention ends — and it cannot be discovered, because the smart bot exposes
  // no REST surface to ask. Left blank, the request omits it entirely so a
  // re-install keeps whatever name the row already carries.
  it("sends the bot's chat name when one is given, and omits it when not", async () => {
    mockRegisterBYO.mockResolvedValue({ id: "i-1" });
    renderUI(<WecomAgentBindButton agentId="agent-1" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aib94");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cret");
    await userEvent.type(screen.getByTestId("wecom-byo-bot-name"), "  Multica Bot  ");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));

    await waitFor(() => expect(mockRegisterBYO).toHaveBeenCalled());
    expect(mockRegisterBYO.mock.calls[0]?.[2].bot_name).toBe("Multica Bot");

    cleanup();
    mockRegisterBYO.mockClear();
    mockRegisterBYO.mockResolvedValue({ id: "i-2" });
    renderUI(<WecomAgentBindButton agentId="agent-2" />);
    await userEvent.click(screen.getByTestId("wecom-agent-connect"));
    await userEvent.type(await screen.findByTestId("wecom-byo-bot-id"), "aib95");
    await userEvent.type(screen.getByTestId("wecom-byo-secret"), "s3cret");
    await userEvent.click(screen.getByTestId("wecom-byo-submit"));

    await waitFor(() => expect(mockRegisterBYO).toHaveBeenCalled());
    expect(mockRegisterBYO.mock.calls[0]?.[2].bot_name).toBeUndefined();
  });

  it("shows the connected badge (not the CTA) when the agent has an active install", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-1", bot_id: "aibot_x", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<WecomAgentBindButton agentId="agent-1" />);
    expect(screen.getByTestId("wecom-agent-bot-connected")).toBeTruthy();
    expect(screen.queryByTestId("wecom-agent-connect")).toBeNull();
  });

  it("treats a revoked install as unbound — shows the CTA, not the connected badge", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-1", bot_id: "aibot_x", status: "revoked" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<WecomAgentBindButton agentId="agent-1" agentName="Bot" />);
    expect(screen.getByTestId("wecom-agent-connect")).toBeTruthy();
    expect(screen.queryByTestId("wecom-agent-bot-connected")).toBeNull();
  });

  it("renders nothing for a non-manager (member)", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    const { container } = renderUI(<WecomAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when install is unavailable and the agent is unbound", () => {
    installationsRef.current = { installations: [], configured: true, install_supported: false };
    const { container } = renderUI(<WecomAgentBindButton agentId="agent-1" />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("WecomTab", () => {
  beforeEach(resetFixtures);

  it("surfaces the not-enabled notice when the deployment has no WeCom key", () => {
    installationsRef.current = { installations: [], configured: false, install_supported: false };
    renderUI(<WecomTab />);
    expect(screen.getByText(/WeCom integration not enabled/i)).toBeTruthy();
  });

  it("shows the empty state when configured but nothing is connected", () => {
    renderUI(<WecomTab />);
    expect(screen.getByText(/No bots connected yet/i)).toBeTruthy();
  });

  it("lists a connected installation with its agent name and a disconnect control", () => {
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", bot_id: "aibot_x", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<WecomTab />);
    expect(screen.getByText("Agent agent-7")).toBeTruthy();
    expect(screen.getByRole("button", { name: /disconnect/i })).toBeTruthy();
  });

  it("confirms before disconnecting, then calls deleteWecomInstallation", async () => {
    mockDeleteInstallation.mockResolvedValue(undefined);
    installationsRef.current = {
      installations: [{ id: "i1", agent_id: "agent-7", bot_id: "aibot_x", status: "active" }],
      configured: true,
      install_supported: true,
    };
    renderUI(<WecomTab />);
    // The row control only opens the confirm dialog — it must NOT delete yet.
    await userEvent.click(screen.getByRole("button", { name: /disconnect/i }));
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
    // Confirm inside the alert dialog.
    const dialog = await screen.findByRole("alertdialog");
    await userEvent.click(within(dialog).getByRole("button", { name: /disconnect/i }));
    await waitFor(() =>
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "i1"),
    );
  });
});
