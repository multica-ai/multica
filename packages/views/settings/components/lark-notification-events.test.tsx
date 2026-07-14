import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { LarkNotificationEventType } from "@multica/core/lark";
import type { LarkInstallation } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { LarkNotificationEvents } from "./lark-notification-events";
import { toast } from "sonner";

const mockUpdateNotificationEvents = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    updateLarkNotificationEvents: mockUpdateNotificationEvents,
  },
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

const ALL_EVENTS: LarkNotificationEventType[] = [
  "issue_assigned",
  "unassigned",
  "assignee_changed",
  "mentioned",
  "new_comment",
  "reaction_added",
  "status_changed",
  "priority_changed",
  "start_date_changed",
  "due_date_changed",
  "task_failed",
  "quick_create_failed",
  "quick_create_done",
  "autopilot_paused",
  "issue_subscribed",
];

function createInstallation(
  overrides: Partial<LarkInstallation> = {},
): LarkInstallation {
  return {
    id: "notification-installation-1",
    workspace_id: "workspace-1",
    app_id: "cli_notification",
    bot_open_id: "ou_notification",
    installer_user_id: "user-1",
    status: "active",
    installation_kind: "notification",
    installed_at: "2026-07-13T00:00:00Z",
    created_at: "2026-07-13T00:00:00Z",
    updated_at: "2026-07-13T00:00:00Z",
    ...overrides,
  };
}

function TestProviders({
  queryClient,
  children,
}: {
  queryClient: QueryClient;
  children: ReactNode;
}) {
  return (
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function renderEvents(
  installation: LarkInstallation,
  canManage = true,
) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
  const result = render(
    <LarkNotificationEvents
      workspaceId="workspace-1"
      installation={installation}
      canManage={canManage}
    />,
    {
      wrapper: ({ children }) => (
        <TestProviders queryClient={queryClient}>{children}</TestProviders>
      ),
    },
  );
  return { ...result, queryClient, invalidateSpy };
}

describe("LarkNotificationEvents", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("enables exactly the five defaults when the persisted field is missing", () => {
    renderEvents(createInstallation());

    const switches = screen.getAllByRole("switch") as HTMLButtonElement[];
    expect(switches).toHaveLength(15);
    expect(switches.filter((control) => control.getAttribute("aria-checked") === "true"))
      .toHaveLength(5);
    expect(screen.getByRole("switch", { name: "Assigned to you" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Mentioned" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Task failed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Quick create failed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Automation paused" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("uses persisted values instead of merging them with defaults", () => {
    renderEvents(
      createInstallation({ notification_event_types: ["new_comment", "due_date_changed"] }),
    );

    expect(screen.getByRole("switch", { name: "New comment" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Due date changed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Assigned to you" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("renders all 15 events in six groups", () => {
    renderEvents(createInstallation());

    expect(screen.getAllByRole("switch")).toHaveLength(15);
    for (const group of [
      "Assignee changes",
      "Comments and mentions",
      "Issue updates",
      "Agent execution",
      "Quick create",
      "Automation",
    ]) {
      expect(screen.getByRole("heading", { name: group })).toBeTruthy();
    }
  });

  it("renders as an unframed section instead of a nested card", () => {
    renderEvents(createInstallation());

    const section = screen.getByRole("region", { name: "Notification events" });
    expect(section).not.toHaveClass("border");
    expect(section).not.toHaveClass("bg-card");
    expect(section).not.toHaveClass("rounded-lg");
  });

  it("lets an admin toggle a label and saves the complete list in catalog order", async () => {
    const user = userEvent.setup();
    mockUpdateNotificationEvents.mockResolvedValue(
      createInstallation({ notification_event_types: ALL_EVENTS }),
    );
    renderEvents(createInstallation());

    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    await user.click(screen.getByText("Unassigned"));
    expect(screen.getByRole("switch", { name: "Unassigned" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await user.click(save);

    await waitFor(() => {
      expect(mockUpdateNotificationEvents).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateNotificationEvents).toHaveBeenCalledWith("workspace-1", [
      "issue_assigned",
      "unassigned",
      "mentioned",
      "task_failed",
      "quick_create_failed",
      "autopilot_paused",
    ]);
  });

  it("disables Save again when a toggle is reverted to the server baseline", async () => {
    const user = userEvent.setup();
    renderEvents(createInstallation({ notification_event_types: ["issue_assigned"] }));

    const save = screen.getByRole("button", { name: "Save" });
    await user.click(screen.getByText("Unassigned"));
    expect(save).toBeEnabled();

    await user.click(screen.getByText("Unassigned"));

    expect(screen.getByRole("switch", { name: "Unassigned" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(save).toBeDisabled();
  });

  it("renders a disabled read-only view for members without a save action", () => {
    renderEvents(createInstallation(), false);

    expect(screen.getByText("Only workspace owners and admins can change these events."))
      .toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    for (const control of screen.getAllByRole("switch")) {
      expect(control).toHaveAttribute("aria-disabled", "true");
    }
  });

  it("shows success feedback and relies on the core mutation to invalidate installations", async () => {
    const user = userEvent.setup();
    mockUpdateNotificationEvents.mockResolvedValue(
      createInstallation({ notification_event_types: ["issue_assigned", "unassigned"] }),
    );
    const { invalidateSpy } = renderEvents(createInstallation());

    await user.click(screen.getByText("Unassigned"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith("Notification events saved");
    });
    expect(invalidateSpy).toHaveBeenCalled();
  });

  it("keeps the draft and shows failure feedback when saving fails", async () => {
    const user = userEvent.setup();
    mockUpdateNotificationEvents.mockRejectedValue(new Error("upstream failed"));
    renderEvents(createInstallation());

    await user.click(screen.getByText("Unassigned"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith("Failed to save notification events");
    });
    expect(screen.getByRole("switch", { name: "Unassigned" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("syncs a clean draft when refreshed installation state changes", async () => {
    const initial = createInstallation({ notification_event_types: ["issue_assigned"] });
    const queryClient = new QueryClient();
    const { rerender } = render(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={initial}
        canManage
      />,
      {
        wrapper: ({ children }) => (
          <TestProviders queryClient={queryClient}>{children}</TestProviders>
        ),
      },
    );

    rerender(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({
          updated_at: "2026-07-13T01:00:00Z",
          notification_event_types: ["due_date_changed"],
        })}
        canManage
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "New comment" })).toHaveAttribute(
        "aria-checked",
        "false",
      );
    });
    expect(screen.getByRole("switch", { name: "Due date changed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("resets a dirty draft when the same installation refreshes externally", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const { rerender } = render(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({ notification_event_types: ["issue_assigned"] })}
        canManage
      />,
      {
        wrapper: ({ children }) => (
          <TestProviders queryClient={queryClient}>{children}</TestProviders>
        ),
      },
    );

    await user.click(screen.getByText("New comment"));
    rerender(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({
          updated_at: "2026-07-13T01:00:00Z",
          notification_event_types: ["due_date_changed"],
        })}
        canManage
      />,
    );

    expect(screen.getByRole("switch", { name: "Assigned to you" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.getByRole("switch", { name: "New comment" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.getByRole("switch", { name: "Due date changed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("resets the draft and clears dirty state when the installation ID changes", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const { rerender } = render(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({ notification_event_types: ["issue_assigned"] })}
        canManage
      />,
      {
        wrapper: ({ children }) => (
          <TestProviders queryClient={queryClient}>{children}</TestProviders>
        ),
      },
    );

    await user.click(screen.getByText("New comment"));
    rerender(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({
          id: "notification-installation-2",
          notification_event_types: ["due_date_changed"],
        })}
        canManage
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "New comment" })).toHaveAttribute(
        "aria-checked",
        "false",
      );
    });
    expect(screen.getByRole("switch", { name: "Due date changed" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("clears dirty state when a save is written back by the server", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
    });
    const savedEvents: LarkNotificationEventType[] = ["issue_assigned", "unassigned"];
    mockUpdateNotificationEvents.mockResolvedValue(
      createInstallation({ notification_event_types: savedEvents }),
    );
    const { rerender } = render(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({ notification_event_types: ["issue_assigned"] })}
        canManage
      />,
      {
        wrapper: ({ children }) => (
          <TestProviders queryClient={queryClient}>{children}</TestProviders>
        ),
      },
    );

    await user.click(screen.getByText("Unassigned"));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockUpdateNotificationEvents).toHaveBeenCalledTimes(1));

    rerender(
      <LarkNotificationEvents
        workspaceId="workspace-1"
        installation={createInstallation({
          updated_at: "2026-07-13T01:00:00Z",
          notification_event_types: savedEvents,
        })}
        canManage
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    });
  });
});
