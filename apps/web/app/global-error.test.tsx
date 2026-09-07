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

let hydratedRoot: Root | undefined;

async function hydrateGlobalError(documentLanguage: string) {
  const reset = vi.fn();
  const onRecoverableError = vi.fn();
  const element = <GlobalError error={new Error("boom")} reset={reset} />;

  document.open();
  document.write(`<!doctype html>${renderToString(element)}`);
  document.close();
  document.documentElement.lang = documentLanguage;

  await act(async () => {
    hydratedRoot = hydrateRoot(document, element, { onRecoverableError });
  });

  return { reset, onRecoverableError };
}

afterEach(async () => {
  await act(async () => {
    hydratedRoot?.unmount();
  });
  hydratedRoot = undefined;
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
    const { reset, onRecoverableError } = await hydrateGlobalError("ja-JP");

    expect(document.documentElement.lang).toBe("ja-JP");
    expect(document.body).toHaveTextContent("問題が発生しました");
    expect(document.body).not.toHaveTextContent("Something went wrong");
    expect(onRecoverableError).not.toHaveBeenCalled();

    await act(async () => {
      document.querySelector("button")?.click();
    });
    expect(reset).toHaveBeenCalledTimes(1);
  });

  it("hydrates in the cookie locale when the browser and document use another language", async () => {
    document.cookie = `${LOCALE_COOKIE}=zh-Hans;path=/`;
    setBrowserLanguages(["ja-JP"]);

    const { onRecoverableError } = await hydrateGlobalError("ja-JP");

    expect(document.documentElement.lang).toBe("zh-CN");
    expect(document.body).toHaveTextContent("出现了问题");
    expect(document.querySelector("button")).toHaveTextContent("重新加载");
    expect(document.body).not.toHaveTextContent("Something went wrong");
    expect(document.body).not.toHaveTextContent("問題が発生しました");
    expect(onRecoverableError).not.toHaveBeenCalled();
  });
});
