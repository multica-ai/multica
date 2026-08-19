import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const mutation = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: {
      preferences: {
        comments: "muted",
        mentions: "all",
        system_notifications: "muted",
      },
    },
  }),
}));
vi.mock("@multica/core/notification-preferences/queries", () => ({
  notificationPreferenceOptions: () => ({
    queryKey: ["notification-preferences"],
  }),
}));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: mutation }),
}));
vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({
    checked,
    onCheckedChange,
    ...props
  }: {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
    "aria-label": string;
  }) => (
    <input
      type="checkbox"
      checked={checked}
      onChange={(event) => onCheckedChange(event.currentTarget.checked)}
      {...props}
    />
  ),
}));
vi.mock("./browser-notification-setting", () => ({
  BrowserNotificationSetting: () => <div>Push permission</div>,
}));
vi.mock("./settings-layout", () => ({
  SettingsTab: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SettingsSection: ({ children }: { children: React.ReactNode }) => (
    <section>{children}</section>
  ),
  SettingsCard: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SettingsRow: ({
    label,
    children,
  }: {
    label: string;
    children: React.ReactNode;
  }) => (
    <label>
      {label}
      {children}
    </label>
  ),
}));
vi.mock("../../i18n", () => ({
  useT: (namespace: string) => ({
    t: (selector: (resource: Record<string, unknown>) => string) => {
      const resources =
        namespace === "inbox"
          ? {
              preferences: {
                groups: {
                  needs_attention: {
                    label: "Needs my attention",
                    description: "",
                  },
                  task_agent_progress: {
                    label: "Task/Agent progress",
                    description: "",
                  },
                  comments_mentions: {
                    label: "Comments & mentions",
                    description: "",
                  },
                  system_health: { label: "System health", description: "" },
                },
                delivery: {
                  title: "Delivery",
                  description: "",
                  label: "Browser alerts & Web Push",
                  hint: "",
                },
              },
            }
          : {
              page: { tabs: { notifications: "Notifications" } },
              notifications: {
                title: "Content",
                description: "",
                toast_failed: "Failed",
              },
              auto_save: { toast_saved: "Saved" },
            };
      return selector(resources);
    },
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab", () => {
  it("renders four collapsed content groups and one independent delivery channel", () => {
    render(<NotificationsTab />);

    for (const label of [
      "Needs my attention",
      "Task/Agent progress",
      "Comments & mentions",
      "System health",
      "Browser alerts & Web Push",
    ]) {
      expect(screen.getByRole("checkbox", { name: label })).toBeTruthy();
    }
    expect(screen.getAllByRole("checkbox")).toHaveLength(5);
    expect(screen.getByText("Push permission")).toBeTruthy();
  });

  it("writes a canonical group override without erasing legacy choices", () => {
    mutation.mockClear();
    render(<NotificationsTab />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: "Comments & mentions" }),
    );

    expect(mutation).toHaveBeenCalledWith(
      expect.objectContaining({
        comments: "muted",
        mentions: "all",
        comments_mentions: "muted",
      }),
      expect.any(Object),
    );
  });
});
