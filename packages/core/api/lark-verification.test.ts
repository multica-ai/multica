// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { parseWithFallback } from "./schema";
import { LarkInstallationSchema, ListLarkInstallationsResponseSchema, EMPTY_LIST_LARK_INSTALLATIONS_RESPONSE } from "./schemas";

const installation = { id: "inst", status: "active" };
describe("Lark receipt response compatibility", () => {
  it("keeps an older response pending verification", () => {
    expect(LarkInstallationSchema.parse(installation).first_message_received_at).toBeUndefined();
  });
  it("accepts a recorded timestamp and future fields/enums", () => {
    const parsed = LarkInstallationSchema.parse({ ...installation, status: "future", future: true, first_message_received_at: "2026-10-29T12:00:00+00:00" });
    expect(parsed.first_message_received_at).toBe("2026-10-29T12:00:00+00:00");
    expect(parsed.status).toBe("future");
  });
  it.each([null, false, 123, "yesterday", "", "2026-99-99T00:00:00Z"])("does not treat %s as verification", (value) => {
    expect(LarkInstallationSchema.parse({ ...installation, first_message_received_at: value }).first_message_received_at).toBeFalsy();
  });
  it("defaults missing status to revoked and malformed lists safely", () => {
    expect(LarkInstallationSchema.parse({ id: "inst" }).status).toBe("revoked");
    expect(parseWithFallback({ installations: "bad" }, ListLarkInstallationsResponseSchema, EMPTY_LIST_LARK_INSTALLATIONS_RESPONSE, { endpoint: "lark" })).toEqual(EMPTY_LIST_LARK_INSTALLATIONS_RESPONSE);
  });
});


afterEach(() => vi.unstubAllGlobals());
it("parses receipt evidence at the API boundary", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ configured: true, installations: [{ ...installation, first_message_received_at: "invalid" }] }), { status: 200 })));
  const response = await new ApiClient("https://api.example.test").listLarkInstallations("ws");
  expect(response.installations[0]?.first_message_received_at).toBeUndefined();
  expect(response.installations[0]?.status).toBe("active");
});
