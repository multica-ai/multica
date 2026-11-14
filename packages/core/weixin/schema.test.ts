// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_WEIXIN_INSTALLATIONS,
  INVALID_WEIXIN_LOGIN,
  ListWeixinInstallationsSchema,
  WeixinLoginSchema,
  isWeixinLoginPending,
} from "./schema";

describe("WeChat API compatibility", () => {
  it("never treats malformed or partial confirmation as a successful binding", () => {
    for (const raw of [
      null,
      [],
      {},
      { id: "login", status: "confirmed" },
      { id: 1, status: "confirmed", installation_id: "bot" },
    ]) {
      expect(
        parseWithFallback(raw, WeixinLoginSchema, INVALID_WEIXIN_LOGIN, {
          endpoint: "weixin/login",
        }),
      ).toEqual(INVALID_WEIXIN_LOGIN);
    }
    expect(
      WeixinLoginSchema.parse({ id: "login", status: "future-status" }).status,
    ).toBe("error");
    expect(
      WeixinLoginSchema.parse({
        id: "login",
        status: "confirmed",
        installation_id: "bot",
      }).status,
    ).toBe("confirmed");
  });
  it("defaults absent deployment capabilities to unavailable", () => {
    expect(ListWeixinInstallationsSchema.parse({})).toEqual(
      EMPTY_WEIXIN_INSTALLATIONS,
    );
    expect(
      parseWithFallback(
        { installations: [null], configured: true },
        ListWeixinInstallationsSchema,
        EMPTY_WEIXIN_INSTALLATIONS,
        { endpoint: "weixin/installations" },
      ),
    ).toEqual(EMPTY_WEIXIN_INSTALLATIONS);
    expect(
      ListWeixinInstallationsSchema.parse({ installations: [{ id: "bot" }] })
        .installations[0]?.status,
    ).toBe("revoked");
  });
  it("stops polling every terminal or unknown status", () => {
    for (const status of [
      "wait",
      "scaned",
      "need_verifycode",
      "scaned_but_redirect",
    ])
      expect(isWeixinLoginPending(status)).toBe(true);
    for (const status of [
      "confirmed",
      "expired",
      "verify_code_blocked",
      "binded_redirect",
      "cancelled",
      "error",
      "new",
    ])
      expect(isWeixinLoginPending(status)).toBe(false);
  });
});
