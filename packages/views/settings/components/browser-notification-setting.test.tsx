import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  capability: vi.fn(),
  ensure: vi.fn(),
  mutate: vi.fn(),
  mutateAsync: vi.fn(),
  deleteSubscription: vi.fn(),
  revoke: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: { enabled: true, public_key: "public-key" } }),
}));
vi.mock("@multica/core/platform", () => ({
  getWebPushCapability: mocks.capability,
  ensureWebPushSubscription: mocks.ensure,
  revokeWebPushSubscription: mocks.revoke,
}));
vi.mock("@multica/core/notification-preferences/web-push", () => ({
  webPushConfigOptions: () => ({ queryKey: ["web-push", "config"] }),
  useUpsertWebPushSubscription: () => ({
    mutate: mocks.mutate,
    mutateAsync: mocks.mutateAsync,
  }),
  useDeleteWebPushSubscription: () => ({ mutate: mocks.deleteSubscription }),
}));
vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
  }: React.PropsWithChildren<{ onClick: () => void }>) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));
vi.mock("./settings-layout", () => ({
  SettingsCard: ({ children }: React.PropsWithChildren) => (
    <div>{children}</div>
  ),
  SettingsRow: ({
    label,
    description,
    children,
  }: React.PropsWithChildren<{
    label: string;
    description: string;
  }>) => (
    <div>
      <span>{label}</span>
      <span>{description}</span>
      {children}
    </div>
  ),
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (resource: Record<string, unknown>) => string) =>
      selector({
        notifications: {
          browser: {
            label: "Background notifications",
            granted: "Ready",
            denied: "Denied",
            hint: "Permission required",
            enable: "Enable",
            enabled_badge: "Enabled",
          },
        },
      }),
  }),
}));

import { BrowserNotificationSetting } from "./browser-notification-setting";

const subscription = {
  endpoint: "https://push.example/subscription",
  expirationTime: null,
  keys: { p256dh: "p256dh", auth: "auth" },
};

describe("BrowserNotificationSetting", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.ensure.mockResolvedValue(null);
    mocks.mutateAsync.mockResolvedValue(undefined);
    mocks.revoke.mockResolvedValue(null);
  });

  it("silently reconciles an already-granted durable subscription", async () => {
    mocks.capability.mockReturnValue("available");
    mocks.ensure.mockResolvedValue(subscription);

    render(<BrowserNotificationSetting />);

    expect(await screen.findByText("Enabled")).toBeTruthy();
    await waitFor(() =>
      expect(mocks.ensure).toHaveBeenCalledWith("public-key", false),
    );
    expect(mocks.mutate).toHaveBeenCalledWith(subscription);
  });

  it("fails closed when browser permission was denied", async () => {
    mocks.capability.mockReturnValue("denied");
    mocks.revoke.mockResolvedValue("https://push.example/revoked");

    render(<BrowserNotificationSetting />);

    expect(await screen.findByText("Denied")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    expect(mocks.ensure).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(mocks.deleteSubscription).toHaveBeenCalledWith(
        "https://push.example/revoked",
      ),
    );
  });

  it("requests Push permission only from the explicit enable action", async () => {
    mocks.capability.mockReturnValue("prompt");
    mocks.ensure.mockResolvedValue(subscription);

    render(<BrowserNotificationSetting />);

    expect(mocks.ensure).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Enable" }));

    await waitFor(() =>
      expect(mocks.ensure).toHaveBeenCalledWith("public-key", true),
    );
    expect(mocks.mutateAsync).toHaveBeenCalledWith(subscription);
  });
});
