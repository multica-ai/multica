import { describe, expect, it } from "vitest";
import { pickContentLang } from "./index";

describe("pickContentLang", () => {
  it("uses the shared locale matcher before selecting persisted content", () => {
    expect(pickContentLang("en-US")).toBe("en");
    expect(pickContentLang("zh-Hant")).toBe("zh");
    expect(pickContentLang("ko-KR")).toBe("ko");
    expect(pickContentLang("ja-JP")).toBe("ja");
  });

  it("falls back to Chinese for unsupported or missing languages", () => {
    expect(pickContentLang("fr-FR")).toBe("zh");
    expect(pickContentLang(null)).toBe("zh");
    expect(pickContentLang(undefined)).toBe("zh");
  });
});
