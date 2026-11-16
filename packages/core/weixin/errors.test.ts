// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { weixinConflict } from "./errors";

describe("WeChat conflict responses", () => {
  it("accepts scoped agent identity and tolerates older or malformed responses", () => {
    expect(weixinConflict(new ApiError("conflict", 409, "Conflict", {
      code: "weixin_already_connected", agent_id: "agent-1", agent_name: "Assistant",
    }))?.agent_name).toBe("Assistant");
    expect(weixinConflict(new ApiError("conflict", 409, "Conflict", {
      code: "weixin_already_connected", agent_name: { secret: "invalid" },
    }))).toEqual({ code: "weixin_already_connected" });
    expect(weixinConflict(new ApiError("conflict", 409, "Conflict"))).toBeUndefined();
    expect(weixinConflict(new Error("offline"))).toBeUndefined();
  });
});
