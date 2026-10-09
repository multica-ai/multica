import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  setCloseBehavior: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

const translations = {
  auto_save: { toast_saved: "Settings saved" },
  desktop: {
    app: {
      title: "Desktop",
      description: "Desktop preferences",
      close_behavior_title: "When closing the main window",
      close_behavior_description: "Choose what closing does",
      close_behavior_tray: "Minimize to system tray",
      close_behavior_taskbar: "Minimize to taskbar",
      close_behavior_quit: "Quit application",
      close_behavior_save_failed: "Failed to save desktop settings",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resources: typeof translations) => string) =>
      selector(translations),
  }),
}));

vi.mock("@multica/ui/components/ui/select", () => ({
  Select: ({
    items,
    value,
    onValueChange,
    disabled,
  }: {
    items: Array<{ value: string; label: string }>;
    value: string;
    onValueChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <select
      aria-label="When closing the main window"
      value={value}
      disabled={disabled}
      onChange={(event) => onValueChange(event.target.value)}
    >
      {items.map((item) => (
        <option key={item.value} value={item.value}>
          {item.label}
        </option>
      ))}
    </select>
  ),
  SelectContent: () => null,
  SelectItem: () => null,
  SelectTrigger: () => null,
  SelectValue: () => null,
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
  },
}));

import { DesktopSettingsTab } from "./desktop-settings-tab";

describe("DesktopSettingsTab", () => {
  beforeEach(() => {
    mocks.get.mockReset().mockResolvedValue({ closeBehavior: "taskbar" });
    mocks.setCloseBehavior
      .mockReset()
      .mockResolvedValue({ closeBehavior: "quit" });
    mocks.toastSuccess.mockReset();
    mocks.toastError.mockReset();
    Object.defineProperty(window, "desktopPreferences", {
      configurable: true,
      value: {
        get: mocks.get,
        setCloseBehavior: mocks.setCloseBehavior,
      },
    });
  });

  it("loads the persisted value and saves a new close behavior", async () => {
    render(<DesktopSettingsTab />);
    const trigger = screen.getByRole("combobox", {
      name: "When closing the main window",
    });

    await waitFor(() => expect(trigger).toHaveValue("taskbar"));
    fireEvent.change(trigger, { target: { value: "quit" } });

    await waitFor(() => {
      expect(mocks.setCloseBehavior).toHaveBeenCalledWith("quit");
      expect(trigger).toHaveValue("quit");
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Settings saved", {
      id: "settings-auto-save",
    });
  });
});
