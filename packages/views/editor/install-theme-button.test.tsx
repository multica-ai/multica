import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  DEFAULT_APPEARANCE,
  THEME_PACKAGE_MAX_BYTES,
  useAppearanceStore,
  useInstalledThemesStore,
  writeZip,
} from "@multica/core/appearance";
import enEditor from "../locales/en/editor.json";
import enSettings from "../locales/en/settings.json";

const getAttachmentBlob = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { getAttachmentBlob } }));

import { InstallThemeButton, canBeThemeZip } from "./install-theme-button";

const RESOURCES = { en: { editor: enEditor, settings: enSettings } };
const Wrapper = ({ children }: { children: ReactNode }) => (
  <I18nProvider locale="en" resources={RESOURCES}>
    {children}
  </I18nProvider>
);

const encode = (text: string) => new TextEncoder().encode(text);
const zip = (files: Record<string, string>) =>
  new Blob([writeZip(Object.entries(files).map(([path, text]) => ({ path, data: encode(text) })))]);

describe("InstallThemeButton", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [], folders: {}, fromStore: {} });
    getAttachmentBlob.mockReset();
  });
  afterEach(cleanup);

  it("is offered on a .zip that is not larger than a theme may be", () => {
    expect(canBeThemeZip("night-sky.zip", 40_000)).toBe(true);
    expect(canBeThemeZip("NIGHT-SKY.ZIP", undefined)).toBe(true);
    expect(canBeThemeZip("backup.zip", THEME_PACKAGE_MAX_BYTES + 1)).toBe(false);
    expect(canBeThemeZip("notes.pdf", 100)).toBe(false);
  });

  it("installs the attachment as a theme after confirmation and switches to it", async () => {
    getAttachmentBlob.mockResolvedValue(
      zip({
        "manifest.json": JSON.stringify({ apiVersion: 1, id: "night-sky", name: "Night Sky", version: "1.0.0" }),
        "theme.css": ".theme-dark { --page-canvas: #101426; }",
      }),
    );
    const user = userEvent.setup();
    render(<InstallThemeButton attachmentId="att-1" filename="night-sky.zip" />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "Install theme" }));
    expect(getAttachmentBlob).toHaveBeenCalledWith("att-1");
    expect(await screen.findByText(/Night Sky/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Install and use" }));

    expect(useInstalledThemesStore.getState().themes.map((theme) => theme.id)).toEqual(["night-sky"]);
    expect(useAppearanceStore.getState().theme).toBe("night-sky");
  });

  it("lists what stops a zip from installing", async () => {
    getAttachmentBlob.mockResolvedValue(zip({ "README.md": "not a theme" }));
    const user = userEvent.setup();
    render(<InstallThemeButton attachmentId="att-2" filename="code.zip" />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "Install theme" }));
    expect(await screen.findByText(enSettings.appearance.import.error_title)).toBeInTheDocument();
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
  });
});
