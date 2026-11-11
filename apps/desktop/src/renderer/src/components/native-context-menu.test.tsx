import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
import type { ContextMenuRequest } from "../../../shared/context-menu";
import { NativeContextMenu } from "./native-context-menu";

const api = vi.hoisted(() => ({
  listener: null as ((request: ContextMenuRequest) => void) | null,
  runContextMenuCommand: vi.fn(),
}));

beforeEach(() => {
  api.listener = null;
  api.runContextMenuCommand.mockClear();
  Object.assign(window, {
    desktopAPI: {
      onContextMenu: (callback: (request: ContextMenuRequest) => void) => {
        api.listener = callback;
        return () => {
          api.listener = null;
        };
      },
      runContextMenuCommand: api.runContextMenuCommand,
    },
  });
});

afterEach(() => {
  delete (window as { desktopAPI?: unknown }).desktopAPI;
});

function renderHost(locale: SupportedLocale = "en") {
  return render(
    <I18nProvider locale={locale} resources={RESOURCES}>
      <NativeContextMenu />
    </I18nProvider>,
  );
}

function request(over: Partial<ContextMenuRequest>): ContextMenuRequest {
  return {
    id: 1,
    x: 10,
    y: 20,
    isEditable: false,
    canCut: false,
    canCopy: false,
    canPaste: false,
    canSelectAll: false,
    suggestions: [],
    canAddToDictionary: false,
    hasLink: false,
    hasImage: false,
    ...over,
  };
}

function open(req: ContextMenuRequest) {
  act(() => api.listener?.(req));
}

describe("NativeContextMenu", () => {
  it("draws only the items the request offers, in the app's language", async () => {
    renderHost("zh-Hans");
    open(request({ isEditable: true, canPaste: true, canSelectAll: true }));

    expect(await screen.findByRole("menuitem", { name: /粘贴/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /全选/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /剪切/ })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /复制/ })).toBeNull();
  });

  it("lists spelling suggestions first and sends the picked index", async () => {
    renderHost();
    open(request({ id: 7, isEditable: true, suggestions: ["the", "tea"], canAddToDictionary: true }));

    const items = await screen.findAllByRole("menuitem");
    expect(items.map((item) => item.textContent)).toEqual(["the", "tea", "Add to dictionary"]);

    fireEvent.click(screen.getByRole("menuitem", { name: "tea" }));
    await waitFor(() =>
      expect(api.runContextMenuCommand).toHaveBeenCalledWith({
        id: 7,
        type: "replaceMisspelling",
        index: 1,
      }),
    );
  });

  it("sends the command once the menu has closed", async () => {
    renderHost();
    open(request({ id: 3, hasLink: true, hasImage: true }));

    fireEvent.click(await screen.findByRole("menuitem", { name: "Copy link address" }));
    await waitFor(() =>
      expect(api.runContextMenuCommand).toHaveBeenCalledWith({ id: 3, type: "copyLink" }),
    );
    expect(api.runContextMenuCommand).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  });

  it("sends nothing when the menu is dismissed", async () => {
    renderHost();
    open(request({ canCopy: true }));

    const menu = await screen.findByRole("menu");
    fireEvent.keyDown(menu, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    expect(api.runContextMenuCommand).not.toHaveBeenCalled();
  });
});
