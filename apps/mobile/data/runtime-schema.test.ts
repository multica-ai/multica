import { describe, expect, it } from "vitest";
import { RuntimeListSchema } from "./schemas";

/**
 * Tests for mobile's CLIENT-SIDE parsing of GET /api/runtimes.
 *
 * These are hand-written fixtures run against `RuntimeListSchema`. They pin
 * how this client REACTS to a given payload — they cannot fail when the Go
 * server starts sending something new, because nothing here executes server
 * code.
 *
 * NEX-38 added 'draining' (safe shutdown in progress) as a first-class server
 * runtime status. The schema must accept it (a draining runtime is alive but
 * refuses new work — the presence dot must keep rendering it) while an
 * unknown status still degrades to 'offline' (the safe, unreachable dot).
 */
describe("runtime list schema", () => {
  const baseRow = {
    id: "rt-1",
    workspace_id: "ws-1",
    name: "Test Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    last_seen_at: "2026-08-10T00:00:00Z",
  };

  it("parses a draining status from a newer server instead of degrading it", () => {
    const parsed = RuntimeListSchema.safeParse([{ ...baseRow, status: "draining" }]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.status).toBe("draining");
  });

  it("keeps online and offline statuses working", () => {
    const parsed = RuntimeListSchema.safeParse([
      { ...baseRow, status: "online" },
      { ...baseRow, id: "rt-2", status: "offline" },
    ]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.status).toBe("online");
    expect(parsed.success && parsed.data[1]?.status).toBe("offline");
  });

  it("degrades an unknown status to offline (safe unreachable dot)", () => {
    // A status this build has never heard of must parse and land on the
    // conservative "unreachable" value rather than fail the whole list parse.
    const parsed = RuntimeListSchema.safeParse([{ ...baseRow, status: "some_future_status" }]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.status).toBe("offline");
  });
});
