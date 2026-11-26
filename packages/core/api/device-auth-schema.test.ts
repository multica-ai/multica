// @vitest-environment node
import { describe, expect, it } from "vitest";
import { DeviceApprovalSchema, DeviceAuthorizationSchema } from "./device-auth-schema";
import { parseWithFallback } from "./schema";

describe("device authorization responses", () => {
  it("defaults absent machine metadata", () => {
    expect(DeviceAuthorizationSchema.parse({})).toEqual({ client_name: "" });
  });

  it("rejects malformed machine metadata", () => {
    expect(
      parseWithFallback({ client_name: 7 }, DeviceAuthorizationSchema, null, { endpoint: "inspect" }),
    ).toBeNull();
  });

  it("accepts only an explicit approval", () => {
    for (const raw of [{}, { status: "pending" }, null]) {
      expect(parseWithFallback(raw, DeviceApprovalSchema, null, { endpoint: "approve" })).toBeNull();
    }
    expect(DeviceApprovalSchema.parse({ status: "approved" })).toEqual({ status: "approved" });
  });
});
