import {
  BrowserWindow,
  Menu,
  MenuItem,
  app,
  clipboard,
  type WebContents,
} from "electron";
import { isSafeExternalHttpUrl, openExternalSafely } from "./external-url";

// Electron ships with no default right-click menu, so a user selecting text
// in the renderer has no way to copy it. Mirror Chrome's minimal clipboard
// menu using `roles`, which keeps i18n + accelerator handling native.
//
// Custom (non-role) link and image items below are NOT auto-localized by
// Electron — roles like "copy" pull labels from the OS, but a custom
// MenuItem only shows the `label` you give it. We translate by OS-preferred
// language so those items at least track Chinese / Japanese / Korean speakers
// alongside the English default; everything else falls through to English,
// which matches Chrome's behavior on those locales without app-level
// translation files.
export function installContextMenu(webContents: WebContents): void {
  webContents.on("context-menu", (_event, params) => {
    const {
      editFlags,
      selectionText,
      isEditable,
      linkURL,
      mediaType,
      hasImageContents,
      misspelledWord,
      dictionarySuggestions,
    } = params;
    const hasSelection = selectionText.trim().length > 0;
    // params.linkURL is the resolved absolute URL of the anchor under the
    // cursor; Electron normalizes relative hrefs against the page URL for
    // us, so we only need to gate on the http(s) scheme allowlist
    // (mirrors openExternalSafely + the renderer's <a> usage). Empty for
    // non-link right-clicks; other schemes (mailto:, javascript:, custom
    // app schemes) are intentionally not surfaced — opening them via
    // shell.openExternal would route through the OS handler and is
    // outside what this menu promises.
    const linkIsHttpUrl = !!linkURL && isSafeExternalHttpUrl(linkURL);
    const labels = pickLabels();

    const menu = new Menu();

    // Spelling: suggestions first, like Chrome. Only in editable fields.
    if (isEditable && misspelledWord) {
      for (const suggestion of dictionarySuggestions) {
        menu.append(
          new MenuItem({
            label: suggestion,
            click: () => webContents.replaceMisspelling(suggestion),
          }),
        );
      }
      menu.append(
        new MenuItem({
          label: labels.addToDictionary,
          click: () =>
            webContents.session.addWordToSpellCheckerDictionary(misspelledWord),
        }),
      );
      menu.append(new MenuItem({ type: "separator" }));
    }

    if (isEditable && editFlags.canCut) {
      menu.append(new MenuItem({ role: "cut" }));
    }
    if (hasSelection && editFlags.canCopy) {
      menu.append(new MenuItem({ role: "copy" }));
    }
    if (isEditable && editFlags.canPaste) {
      menu.append(new MenuItem({ role: "paste" }));
    }
    if (isEditable && editFlags.canSelectAll) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: "separator" }));
      }
      menu.append(new MenuItem({ role: "selectAll" }));
    }

    // Link items — only when the cursor is over an actual http(s) <a>.
    // Without these the renderer's <a target="_blank"> gives users no
    // standard right-click affordance ("Open in new window", "Copy link
    // address"); the default click handler does forward to
    // setWindowOpenHandler → openExternalSafely, but discoverability via
    // the keyboard / mouse context menu was missing.
    if (linkIsHttpUrl) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: "separator" }));
      }
      menu.append(
        new MenuItem({
          label: labels.openLink,
          click: () => {
            // openExternalSafely re-validates the scheme — defense in
            // depth in case Electron ever surfaces a non-http linkURL
            // we forgot to filter at this layer.
            void openExternalSafely(linkURL);
          },
        }),
      );
      menu.append(
        new MenuItem({
          label: labels.copyLinkAddress,
          click: () => {
            // Electron 44 made clipboard writes async (W3C Clipboard API
            // alignment); catch so a failed write can't surface as an
            // unhandled rejection in the main process.
            clipboard.writeText(linkURL).catch((err: unknown) => {
              console.warn("[context-menu] copy link address failed", err);
            });
          },
        }),
      );
    }

    // Image items — only for an <img> whose pixels have loaded. Copies the
    // decoded bitmap Chromium already holds, so it works for any format and
    // any origin (signed CDN URLs, blob: URLs) without refetching.
    if (mediaType === "image" && hasImageContents) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: "separator" }));
      }
      menu.append(
        new MenuItem({
          label: labels.copyImage,
          click: () => {
            webContents.copyImageAt(params.x, params.y);
          },
        }),
      );
    }

    if (menu.items.length === 0) return;
    const window = BrowserWindow.fromWebContents(webContents) ?? undefined;
    menu.popup({ window });
  });
}

// Labels for the custom menu items in the user's OS-preferred language,
// with English as the fallback. Kept inline because the main process has
// no shared i18n loader (the renderer's i18next is per-window and not
// reachable from here), and pulling one in for three strings would be
// more rope than payload. Matches the four locales the renderer ships.
type ContextMenuLabels = {
  openLink: string;
  copyLinkAddress: string;
  copyImage: string;
  addToDictionary: string;
};

const labelsByLocale: Record<string, ContextMenuLabels> = {
  en: {
    openLink: "Open Link in Browser",
    copyLinkAddress: "Copy Link Address",
    copyImage: "Copy Image",
    addToDictionary: "Add to Dictionary",
  },
  "zh-Hans": {
    openLink: "在浏览器中打开链接",
    copyLinkAddress: "复制链接地址",
    copyImage: "复制图片",
    addToDictionary: "添加到词典",
  },
  "zh-Hant": {
    openLink: "在瀏覽器中開啟連結",
    copyLinkAddress: "複製連結網址",
    copyImage: "複製圖片",
    addToDictionary: "加入字典",
  },
  ja: {
    openLink: "ブラウザでリンクを開く",
    copyLinkAddress: "リンクのアドレスをコピー",
    copyImage: "画像をコピー",
    addToDictionary: "辞書に追加",
  },
  ko: {
    openLink: "브라우저에서 링크 열기",
    copyLinkAddress: "링크 주소 복사",
    copyImage: "이미지 복사",
    addToDictionary: "사전에 추가",
  },
};

// pickLabels resolves the OS-preferred language to one of the four
// locales we ship copy for. We say "Open Link in Browser" rather than
// "Open Link in New Window" because the link is opened via
// shell.openExternal — it lands in the user's default browser, not in
// another Multica window — so the wording matches what actually
// happens.
function pickLabels(): ContextMenuLabels {
  const preferred = app.getPreferredSystemLanguages()[0]?.toLowerCase() ?? "";
  if (preferred.startsWith("zh")) {
    // Traditional-script and Taiwan/Hong Kong/Macau tags get zh-Hant; every
    // other Chinese variant gets the Simplified copy.
    return /^zh-(hant|tw|hk|mo)\b/.test(preferred)
      ? labelsByLocale["zh-Hant"]
      : labelsByLocale["zh-Hans"];
  }
  if (preferred.startsWith("ja")) return labelsByLocale.ja;
  if (preferred.startsWith("ko")) return labelsByLocale.ko;
  return labelsByLocale.en;
}
