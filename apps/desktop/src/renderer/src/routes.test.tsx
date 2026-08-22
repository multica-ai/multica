import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const translations = {
  desktop: {
    group_title: "Desktop",
    tabs: {
      daemon: "Daemon",
      updates: "Updates",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resources: typeof translations) => string) =>
      selector(translations),
  }),
}));

vi.mock("@multica/views/settings", () => ({
  SettingsPage: ({
    extraSettingsGroups = [],
  }: {
    extraSettingsGroups?: Array<{
      label: string;
      tabs: Array<{ value: string; label: string; content: ReactNode }>;
    }>;
  }) => (
    <div>
      {extraSettingsGroups.map((group) => (
        <section key={group.label}>
          <h2>{group.label}</h2>
          {group.tabs.map((tab) => (
            <div key={tab.value}>
              <span>{tab.label}</span>
              {tab.content}
            </div>
          ))}
        </section>
      ))}
    </div>
  ),
}));

vi.mock("./components/daemon-settings-tab", () => ({
  DaemonSettingsTab: () => <div>Daemon settings content</div>,
}));

vi.mock("./components/updates-settings-tab", () => ({
  UpdatesSettingsTab: () => <div>Updates settings content</div>,
}));

import { DesktopSettingsRoute } from "./routes";

describe("DesktopSettingsRoute", () => {
  it("injects desktop settings as a platform group in menu order", () => {
    render(<DesktopSettingsRoute />);

    expect(screen.getByRole("heading", { name: "Desktop" })).toBeVisible();
    expect(screen.getByText("Daemon")).toBeVisible();
    expect(screen.getByText("Updates")).toBeVisible();
    expect(screen.getByText("Daemon settings content")).toBeVisible();
    expect(screen.getByText("Updates settings content")).toBeVisible();
  });
});
