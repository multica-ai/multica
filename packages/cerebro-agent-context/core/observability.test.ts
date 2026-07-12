import { describe, expect, it } from "vitest";
import { normalizeAgentObservability } from "./observability";

describe("normalizeAgentObservability", () => {
  it("maps a well-formed response through unchanged", () => {
    const raw = {
      agent_id: "a1",
      agent_name: "Mia",
      context_version: "1.4.0",
      version_count: 12,
      versions_last_30d: 3,
      last_changed_at: "2026-07-10T09:00:00Z",
      change_requests: {
        pending: 1,
        approved: 4,
        rejected: 2,
        merged: 5,
        total: 12,
      },
      approvers: [
        {
          user_id: "u1",
          name: "Jesper",
          approved: 3,
          merged: 2,
          rejected: 1,
          total: 6,
        },
      ],
      drift: { total: 2, errors: 1, warnings: 1, infos: 0 },
    };
    const out = normalizeAgentObservability(raw);
    expect(out).toEqual(raw);
  });

  it("degrades a completely empty body to zeros, never throws", () => {
    const out = normalizeAgentObservability({});
    expect(out.version_count).toBe(0);
    expect(out.versions_last_30d).toBe(0);
    expect(out.last_changed_at).toBeNull();
    expect(out.change_requests.total).toBe(0);
    expect(out.approvers).toEqual([]);
    expect(out.drift.total).toBe(0);
  });

  it("survives null / wrong-typed fields (drifted server shape)", () => {
    const out = normalizeAgentObservability({
      agent_id: 123, // wrong type
      version_count: "lots", // wrong type
      last_changed_at: 0, // wrong type → null
      change_requests: null,
      approvers: "nope", // not an array → []
      drift: undefined,
    });
    expect(out.agent_id).toBe("");
    expect(out.version_count).toBe(0);
    expect(out.last_changed_at).toBeNull();
    expect(out.change_requests.pending).toBe(0);
    expect(out.approvers).toEqual([]);
    expect(out.drift.errors).toBe(0);
  });

  it("normalizes each approver entry defensively", () => {
    const out = normalizeAgentObservability({
      approvers: [{ user_id: "u1" }, { name: "Bob", approved: 2 }, null],
    });
    expect(out.approvers).toHaveLength(3);
    expect(out.approvers[0]).toEqual({
      user_id: "u1",
      name: "",
      approved: 0,
      merged: 0,
      rejected: 0,
      total: 0,
    });
    expect(out.approvers[1]!.name).toBe("Bob");
    expect(out.approvers[1]!.approved).toBe(2);
    expect(out.approvers[2]!.user_id).toBe("");
  });

  it("rejects non-finite numbers (NaN/Infinity) down to 0", () => {
    const out = normalizeAgentObservability({
      version_count: NaN,
      versions_last_30d: Infinity,
    });
    expect(out.version_count).toBe(0);
    expect(out.versions_last_30d).toBe(0);
  });
});
