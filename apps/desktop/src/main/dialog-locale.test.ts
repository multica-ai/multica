import { describe, expect, it } from "vitest";
import { resolveDialogLocale } from "./dialog-locale";

describe("resolveDialogLocale", () => {
  it("parses exact BCP-47 language subtags without prefix collisions", () => {
    expect(resolveDialogLocale("ko")).toBe("ko");
    expect(resolveDialogLocale("ko-KR")).toBe("ko");
    expect(resolveDialogLocale("kok")).toBe("en");
    expect(resolveDialogLocale("kok-IN")).toBe("en");
    expect(resolveDialogLocale("ja-JP")).toBe("ja");
    expect(resolveDialogLocale("fr-CA")).toBe("fr");
    expect(resolveDialogLocale("es-MX")).toBe("es");
    expect(resolveDialogLocale("ru-RU")).toBe("ru");
  });

  it("distinguishes Simplified and Traditional Chinese across hyphen and underscore separators", () => {
    expect(resolveDialogLocale("zh-CN")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh_CN")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh-Hans-SG")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh-TW")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh_TW")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh-Hant-HK")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh_MO")).toBe("zh-Hant");
  });

  it("falls back to English for unknown or empty locales", () => {
    expect(resolveDialogLocale("")).toBe("en");
    expect(resolveDialogLocale("de-DE")).toBe("en");
  });
});
