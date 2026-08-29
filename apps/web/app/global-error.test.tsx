// @vitest-environment jsdom

import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LOCALE_COOKIE } from "@multica/core/i18n";
import { resolveEmergencyLocale } from "./emergency-locale";
import GlobalError from "./global-error";

vi.mock("@multica/core/analytics", () => ({
  captureException: vi.fn(),
}));

function setBrowserLanguages(languages: string[]) {
  Object.defineProperty(window.navigator, "languages", {
    configurable: true,
    value: languages,
  });
}

function resetDocument() {
  document.open();
  document.write("<!doctype html><html><head></head><body></body></html>");
  document.close();
}

afterEach(() => {
  document.cookie = `${LOCALE_COOKIE}=;path=/;max-age=0`;
  setBrowserLanguages(["en-US"]);
  resetDocument();
});

describe("resolveEmergencyLocale", () => {
  it("prefers the product locale cookie over browser languages", () => {
    document.cookie = `${LOCALE_COOKIE}=zh-Hans;path=/`;
    setBrowserLanguages(["ja-JP"]);

    expect(resolveEmergencyLocale()).toBe("zh-Hans");
  });

  it("hydrates with the browser locale when no cookie exists", async () => {
    setBrowserLanguages(["ja-JP", "ja"]);
    const error = new Error("boom");
    const element = <GlobalError error={error} reset={vi.fn()} />;
    const serverMarkup = renderToString(element);

    document.open();
    document.write(`<!doctype html>${serverMarkup}`);
    document.close();
    document.documentElement.lang = "ja-JP";

    let root: Root | undefined;
    await act(async () => {
      root = hydrateRoot(document, element);
    });

    expect(document.documentElement.lang).toBe("ja-JP");
    expect(document.body).toHaveTextContent("問題が発生しました");
    expect(document.body).not.toHaveTextContent("Something went wrong");

    await act(async () => {
      root?.unmount();
    });
  });
});
