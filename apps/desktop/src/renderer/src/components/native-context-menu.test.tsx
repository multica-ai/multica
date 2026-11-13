import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "@multica/views/locales";
import { useContextMenuScope, type ContextMenuScope } from "@multica/views/common/context-menu-scope";
import type { ContextMenuRequest } from "../../../shared/context-menu";
import { NativeContextMenu, tableToMarkdown } from "./native-context-menu";

const api = vi.hoisted(() => ({
  listener: null as ((request: ContextMenuRequest) => void) | null,
  runContextMenuCommand: vi.fn(),
  copyText: vi.fn(async (_text: string) => true),
}));

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: api.copyText }));

beforeEach(() => {
  api.listener = null;
  api.runContextMenuCommand.mockClear();
  api.copyText.mockClear();
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

function renderHost(locale: SupportedLocale = "en", page?: React.ReactNode) {
  return render(
    <I18nProvider locale={locale} resources={RESOURCES}>
      {page}
      <NativeContextMenu />
    </I18nProvider>,
  );
}

function Scope({ read, children }: { read: () => Partial<ContextMenuScope> | null; children: React.ReactNode }) {
  return <div ref={useContextMenuScope(read)}>{children}</div>;
}

/** A DOM right-click on `target`, then main's request for it. */
function rightClick(target: Element, req: ContextMenuRequest) {
  fireEvent.contextMenu(target, { clientX: 5, clientY: 5 });
  open(req);
}

function menuLabels() {
  return screen.getAllByRole("menuitem").map((item) => item.textContent);
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

  it("draws nothing for a right-click neither main nor the page has items for", () => {
    renderHost("en", <p>plain</p>);
    rightClick(screen.getByText("plain"), request({}));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("offers Copy code on a code block and copies its text", async () => {
    renderHost("en", <pre><code>const a = 1;</code></pre>);
    rightClick(screen.getByText("const a = 1;"), request({}));

    fireEvent.click(await screen.findByRole("menuitem", { name: "Copy code" }));
    await waitFor(() => expect(api.copyText).toHaveBeenCalledWith("const a = 1;"));
    expect(api.runContextMenuCommand).not.toHaveBeenCalled();
  });

  it("copies a table as Markdown", () => {
    const table = document.createElement("table");
    table.innerHTML = "<tr><th>Name</th><th>A|B</th></tr><tr><td>x</td><td>y</td></tr>";
    expect(tableToMarkdown(table)).toBe("| Name | A\\|B |\n| --- | --- |\n| x | y |");
  });

  it("follows the content with the surrounding object's actions in one submenu", async () => {
    const reply = vi.fn();
    renderHost(
      "en",
      <Scope
        read={() => ({
          label: "Comment actions",
          icon: null,
          groups: [[{ key: "reply", icon: null, label: "Reply", onSelect: reply }]],
          selectionActions: (text) => [
            { key: "issue", icon: null, label: `Create issue: ${text}`, onSelect: vi.fn() },
          ],
        })}
      >
        <a href="https://example.com">a link</a>
      </Scope>,
    );
    rightClick(screen.getByText("a link"), request({ hasLink: true }));

    await screen.findByRole("menu");
    expect(screen.getByText("https://example.com/")).toBeInTheDocument();
    expect(menuLabels()).toEqual(["Open link in browser", "Copy link address", "Comment actions"]);

    fireEvent.click(screen.getByRole("menuitem", { name: "Comment actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Reply" }));
    await waitFor(() => expect(reply).toHaveBeenCalledTimes(1));
  });

  it("adds the scope's selection actions after Copy", async () => {
    renderHost(
      "en",
      <Scope read={() => ({ selectionActions: (text) => [{ key: "a", icon: null, label: `Add ${text}`, onSelect: vi.fn() }] })}>
        <p>selected words</p>
      </Scope>,
    );
    const text = screen.getByText("selected words");
    const range = document.createRange();
    range.selectNodeContents(text);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    rightClick(text, request({ canCopy: true }));

    await screen.findByRole("menu");
    expect(menuLabels()).toEqual([expect.stringMatching(/^Copy/), "Add selected words"]);
  });

  it("leaves out selection entries for the word a right-click selects by itself", async () => {
    renderHost(
      "en",
      <Scope read={() => ({ selectionActions: () => [{ key: "a", icon: null, label: "Add", onSelect: vi.fn() }] })}>
        <p>a word</p>
      </Scope>,
    );
    window.getSelection()!.removeAllRanges();
    const text = screen.getByText("a word");
    fireEvent.contextMenu(text, { clientX: 5, clientY: 5 });
    // macOS selects the word as the right-click's default action.
    const range = document.createRange();
    range.selectNodeContents(text);
    window.getSelection()!.addRange(range);
    open(request({ canCopy: true }));

    await screen.findByRole("menu");
    expect(menuLabels()).toEqual([expect.stringMatching(/^Copy/)]);
    window.getSelection()!.removeAllRanges();
  });

  it("lets an attachment's own actions replace the link entries", async () => {
    renderHost(
      "en",
      <Scope read={() => ({ contentActions: [{ key: "preview", icon: null, label: "Preview", onSelect: vi.fn() }] })}>
        <a href="https://cdn.example/file.pdf?sig=1">file.pdf</a>
      </Scope>,
    );
    rightClick(screen.getByText("file.pdf"), request({ hasLink: true }));

    await screen.findByRole("menu");
    expect(menuLabels()).toEqual(["Preview"]);
  });

  it("keeps a text field to its edit menu", async () => {
    renderHost(
      "en",
      <Scope read={() => ({ label: "Comment actions", icon: null, groups: [[{ key: "r", icon: null, label: "Reply", onSelect: vi.fn() }]] })}>
        <textarea aria-label="field" />
      </Scope>,
    );
    rightClick(screen.getByLabelText("field"), request({ isEditable: true, canPaste: true }));

    await screen.findByRole("menu");
    expect(menuLabels()).toEqual([expect.stringMatching(/^Paste/)]);
  });

  it("opens the focused element's menu on Shift+F10", () => {
    const onContextMenu = vi.fn((event: React.MouseEvent) => event.preventDefault());
    renderHost("en", <button type="button" onContextMenu={onContextMenu}>row</button>);
    const button = screen.getByText("row");
    button.focus();

    expect(fireEvent.keyDown(button, { key: "F10", shiftKey: true })).toBe(false);
    expect(onContextMenu).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(button, { key: "F10" });
    expect(onContextMenu).toHaveBeenCalledTimes(1);
  });

  it("does not suppress the native key when no DOM handler claims the link", () => {
    renderHost("en", <a href="https://example.com">regular link</a>);
    const link = screen.getByRole("link");
    link.focus();
    const handled = fireEvent.keyDown(link, { key: "F10", shiftKey: true });
    expect({ nativeAllowed: handled, customMenu: !!screen.queryByRole("menu") }).toEqual({ nativeAllowed: true, customMenu: false });
  });
});
