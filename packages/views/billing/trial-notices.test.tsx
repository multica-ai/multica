import { act, fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceTrialView } from "@multica/core/billing";
import { useTrialNoticeStore } from "@multica/core/billing";
import { useSessionPromptStore } from "@multica/core/session-prompt";
import type { AutopilotQuotaUsage } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { WorkspaceTrialNotices } from "./trial-notices";

const DAY = 24 * 60 * 60 * 1000;
// Ended trials in these tests ended two days ago, so they have settled.
const ENDS_AT = Date.now() - 2 * DAY;

const h = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return {
    trial: { kind: "none" } as WorkspaceTrialView,
    workspace: { id: "ws-1", name: "Acme Labs" },
    role: "owner" as "owner" | "admin" | "member" | null,
    features: { private_projects: "available" } as Record<string, string>,
    usage: null as AutopilotQuotaUsage | null,
    /** The feature read failed after its retries. */
    featuresFailed: false,
    /** A failed feature read is being retried. */
    featuresRetrying: false,
    /** When the mocked feature and usage answers were received. */
    dataUpdatedAt: Date.now(),
    push: vi.fn(),
    toast: vi.fn(),
    chat: create<{ isOpen: boolean; floatingChatEnabled: boolean }>(() => ({
      isOpen: false,
      floatingChatEnabled: true,
    })),
  };
});

vi.mock("@multica/core/billing", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/billing")>("@multica/core/billing");
  return { ...actual, useWorkspaceTrial: () => h.trial };
});
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => true }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => h.workspace,
  useWorkspacePaths: () => ({ settings: () => "/acme/settings", chat: () => "/acme/chat" }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ userId: "u1", role: h.role, member: null, isLoading: false }),
}));
vi.mock("@multica/core/chat", () => ({ useChatStore: h.chat }));
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      const key = options.queryKey;
      if (key[0] === "workspace-features") {
        if (h.featuresFailed) {
          return { data: undefined, isPending: false, isError: true, isFetching: h.featuresRetrying, dataUpdatedAt: 0 };
        }
        return { data: { features: h.features }, isPending: false, dataUpdatedAt: h.dataUpdatedAt };
      }
      if (key[key.length - 1] === "usage") {
        return { data: h.usage, isPending: false, dataUpdatedAt: h.dataUpdatedAt };
      }
      return { data: undefined, isPending: true };
    },
  };
});
vi.mock("sonner", () => ({ toast: h.toast }));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: "/acme/issues", push: h.push }),
}));

function active(overrides: Partial<Extract<WorkspaceTrialView, { kind: "active" }>> = {}): WorkspaceTrialView {
  return {
    kind: "active",
    plan: "business",
    startedAt: ENDS_AT - 14 * DAY,
    endsAt: ENDS_AT,
    daysLeft: 9,
    day: 6,
    totalDays: 14,
    warning: false,
    reminder: null,
    ...overrides,
  };
}

const ended: WorkspaceTrialView = {
  kind: "ended",
  plan: "business",
  endsAt: ENDS_AT,
  recent: true,
  confirmed: true,
  settledAt: ENDS_AT + 70_000,
  settled: true,
};

beforeEach(() => {
  h.trial = { kind: "none" };
  h.workspace = { id: "ws-1", name: "Acme Labs" };
  h.role = "owner";
  h.features = { private_projects: "available" };
  h.usage = null;
  h.dataUpdatedAt = Date.now();
  h.featuresFailed = false;
  h.featuresRetrying = false;
  h.push.mockReset();
  h.toast.mockReset();
  h.chat.setState({ isOpen: false, floatingChatEnabled: true });
  useTrialNoticeStore.setState({ seen: [] });
  useSessionPromptStore.setState({ corner: null });
});

describe("WorkspaceTrialNotices", () => {
  it("preserves the owner's end notice until membership is known", () => {
    h.role = null;
    h.trial = ended;
    const { rerender } = renderWithI18n(<WorkspaceTrialNotices />);
    h.role = "owner";
    rerender(<WorkspaceTrialNotices />);
    expect.soft(h.toast).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeInTheDocument();
  });

  it("shows nothing outside a trial or between reminders", () => {
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    h.trial = active();
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("reminds owners and admins once per milestone", () => {
    h.trial = active({ daysLeft: 3, warning: true, reminder: 3 });
    const { unmount } = renderWithI18n(<WorkspaceTrialNotices />);

    expect(screen.getByRole("status")).toHaveTextContent("3 days left in your Business trial.");
    fireEvent.click(screen.getByRole("button", { name: "Later" }));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    unmount();

    // Seen on this device: a remount stays quiet until the next milestone.
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("opens Billing from the reminder", () => {
    h.trial = active({ daysLeft: 7, reminder: 7 });
    renderWithI18n(<WorkspaceTrialNotices />);

    expect(screen.getByRole("status")).toHaveTextContent(
      "Your Business trial is halfway through: 7 days left.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Choose a plan" }));
    expect(h.push).toHaveBeenCalledWith("/acme/settings?tab=billing");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("does not remind members", () => {
    h.role = "member";
    h.trial = active({ daysLeft: 1, warning: true, reminder: 1 });
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("welcomes everyone on the first day, once", () => {
    h.role = "member";
    h.trial = active({ daysLeft: 14, day: 1 });
    const { unmount } = renderWithI18n(<WorkspaceTrialNotices />);

    expect(
      screen.getByRole("heading", { name: "Welcome! This workspace has a free 14-day Business trial" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(useSessionPromptStore.getState().corner).toBeNull();
    unmount();

    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("leaves the corner to a card already showing there", () => {
    useSessionPromptStore.setState({ corner: "whats-new" });
    h.trial = active({ daysLeft: 14, day: 1 });
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("shows the welcome once another corner card closes", () => {
    useSessionPromptStore.setState({ corner: "whats-new" });
    h.trial = active({ daysLeft: 14, day: 1 });
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();

    act(() => useSessionPromptStore.getState().releaseCorner("whats-new"));
    expect(
      screen.getByRole("heading", { name: "Welcome! This workspace has a free 14-day Business trial" }),
    ).toBeInTheDocument();
    expect(useSessionPromptStore.getState().corner).toBe("trial");
  });

  it("waits for the end to settle before telling a manager what changed", () => {
    h.trial = { ...ended, settled: false } as WorkspaceTrialView;
    h.features = { private_projects: "locked" };
    const { unmount } = renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    unmount();

    // Settled, but the cached answers predate it: wait for fresh ones.
    h.trial = ended;
    h.dataUpdatedAt = ENDS_AT;
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(useTrialNoticeStore.getState().seen).toEqual([]);
  });

  it("tells a manager what changed, listing only restrictions in force", () => {
    h.trial = ended;
    h.features = { private_projects: "locked" };
    h.usage = {
      action: "enforce",
      used: 1284,
      reserved: 0,
      total: 1284,
      limit: 200,
      reached: false,
      period_start: null,
      period_end: null,
      reset_at: null,
      blocked_counts: null,
    };
    renderWithI18n(<WorkspaceTrialNotices />);

    const dialog = screen.getByRole("dialog", { name: "Your Business trial has ended" });
    expect(dialog).toHaveTextContent("Acme Labs is now on Free. Nothing was deleted.");
    expect(dialog).toHaveTextContent("Members, projects, and settings you already have stay as they are.");
    expect(dialog).toHaveTextContent("Private projects stay private.");
    expect(dialog).toHaveTextContent("Autopilots get 200 runs a month; 1,284 used this month.");

    fireEvent.click(screen.getByRole("button", { name: "Continue on Free" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens on Cloud's confirmation even when a detail read failed", () => {
    h.trial = ended;
    h.featuresFailed = true;
    h.usage = {
      action: "enforce",
      used: 12,
      reserved: 0,
      total: 12,
      limit: 200,
      reached: false,
      period_start: null,
      period_end: null,
      reset_at: null,
      blocked_counts: null,
    };
    renderWithI18n(<WorkspaceTrialNotices />);

    const dialog = screen.getByRole("dialog", { name: "Your Business trial has ended" });
    expect(dialog).toHaveTextContent("Autopilots get 200 runs a month; 12 used this month.");
    expect(dialog).not.toHaveTextContent("Private projects");
  });

  it("stays open while a failed detail read is retried", () => {
    h.trial = ended;
    h.featuresFailed = true;
    const { rerender } = renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    h.featuresRetrying = true;
    rerender(<WorkspaceTrialNotices />);
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    h.featuresFailed = false;
    h.features = { private_projects: "locked" };
    rerender(<WorkspaceTrialNotices />);
    expect(screen.getByRole("dialog")).toHaveTextContent("Private projects stay private.");
  });

  it("claims no restriction while limits are only observed", () => {
    h.trial = ended;
    h.usage = {
      action: "observe",
      used: 10,
      reserved: 0,
      total: 10,
      limit: 200,
      reached: false,
      period_start: null,
      period_end: null,
      reset_at: null,
      blocked_counts: null,
    };
    renderWithI18n(<WorkspaceTrialNotices />);

    const dialog = screen.getByRole("dialog");
    expect(dialog).not.toHaveTextContent("Private projects");
    expect(dialog).not.toHaveTextContent("Automations");
  });

  it("tells members once with a toast", () => {
    h.role = "member";
    h.trial = ended;
    const { unmount } = renderWithI18n(<WorkspaceTrialNotices />);
    expect(h.toast).toHaveBeenCalledTimes(1);
    expect(h.toast).toHaveBeenCalledWith("This workspace is now on Free", {
      description: "Existing settings keep working. Ask a workspace admin if you need more.",
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    unmount();

    act(() => {
      renderWithI18n(<WorkspaceTrialNotices />);
    });
    expect(h.toast).toHaveBeenCalledTimes(1);
  });

  it("tells a member about each workspace that ended, across a workspace switch", () => {
    h.role = "member";
    h.trial = ended;
    const { rerender } = renderWithI18n(<WorkspaceTrialNotices />);
    expect(h.toast).toHaveBeenCalledTimes(1);

    h.workspace = { id: "ws-2", name: "Pixel Forge" };
    rerender(<WorkspaceTrialNotices />);
    expect(h.toast).toHaveBeenCalledTimes(2);
  });

  it("announces nothing while only this clock says the trial ended", () => {
    h.trial = { ...ended, confirmed: false } as WorkspaceTrialView;
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    h.role = "member";
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(h.toast).not.toHaveBeenCalled();
    expect(useTrialNoticeStore.getState().seen).toEqual([]);
  });

  it("stays quiet once the week after the end has passed", () => {
    h.trial = { ...ended, recent: false } as WorkspaceTrialView;
    renderWithI18n(<WorkspaceTrialNotices />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(h.toast).not.toHaveBeenCalled();
  });
});
