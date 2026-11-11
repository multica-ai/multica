import { describe, expect, it } from "vitest";
import { TaskContextReceiptResponseSchema } from "./schemas";

describe("TaskContextReceiptResponseSchema", () => {
  it("passes a null receipt through", () => {
    expect(TaskContextReceiptResponseSchema.parse({ receipt: null })).toEqual({ receipt: null });
  });

  it("keeps unknown enum values and drops malformed source rows", () => {
    const parsed = TaskContextReceiptResponseSchema.parse({
      receipt: {
        schema_version: 1,
        runtime: "qwen",
        prompt_bytes: 10,
        brief_bytes: 20,
        brief_delivery: "file",
        resumed_session: false,
        mcp_servers: ["github"],
        sources: [
          { kind: "runtime_brief", bytes: 20, delivery: "file", path: "QWEN.md", load: "probed_by_future_daemon" },
          { bytes: 5 },
          "not an object",
        ],
      },
      recorded_at: "2026-11-11T11:29:00Z",
    });
    expect(parsed.receipt?.sources).toHaveLength(1);
    expect(parsed.receipt?.sources[0]?.load).toBe("probed_by_future_daemon");
    expect(parsed.recorded_at).toBe("2026-11-11T11:29:00Z");
  });

  it("defaults missing lists so the view can map them", () => {
    const parsed = TaskContextReceiptResponseSchema.parse({ receipt: { schema_version: 1, runtime: "claude" } });
    expect(parsed.receipt?.mcp_servers).toEqual([]);
    expect(parsed.receipt?.sources).toEqual([]);
    expect(parsed.receipt?.resumed_session).toBe(false);
  });
});
