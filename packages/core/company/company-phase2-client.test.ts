import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const client = () => new ApiClient("https://api.test");

describe("ApiClient brief push preference", () => {
  it("GET /api/company/brief-push reads the caller's preference", async () => {
    const fetchMock = respond({ brief_push: true, binding_id: "b1", quiet_hours: { start: 22, end: 7 } });
    expect(await client().getBriefPushPreference()).toEqual({
      brief_push: true,
      binding_id: "b1",
      quiet_hours: { start: 22, end: 7 },
    });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/company/brief-push");
  });

  it("PUT /api/company/brief-push sends all three fields and reads the saved preference", async () => {
    const fetchMock = respond({ brief_push: true, binding_id: "b1", quiet_hours: null });
    const saved = await client().updateBriefPushPreference({ brief_push: true, binding_id: "b1", quiet_hours: null });
    expect(saved).toMatchObject({ brief_push: true, binding_id: "b1", quiet_hours: null });
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      brief_push: true,
      binding_id: "b1",
      quiet_hours: null,
    });
  });

  it("falls back to off when the GET body is malformed", async () => {
    respond({ brief_push: "sometimes" });
    expect(await client().getBriefPushPreference()).toMatchObject({ brief_push: false, binding_id: null });
  });
});

describe("ApiClient brief push bindings", () => {
  it("GET /api/company/brief-push/bindings reads the caller's chats", async () => {
    const fetchMock = respond([{ id: "b1", channel: "telegram", label: "Ops room" }]);
    expect(await client().listBriefPushBindings()).toEqual([
      { id: "b1", channel: "telegram", label: "Ops room" },
    ]);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/company/brief-push/bindings");
  });

  it("reads an empty list when no chat is linked", async () => {
    respond([]);
    expect(await client().listBriefPushBindings()).toEqual([]);
  });

  it("falls back to an empty list when the body is malformed", async () => {
    respond({ bindings: "telegram" });
    expect(await client().listBriefPushBindings()).toEqual([]);
  });

  it("drops a malformed entry and keeps the valid ones", async () => {
    respond([{ channel: "telegram", label: "No id" }, { id: "b2", channel: "telegram", label: "Kept" }]);
    expect(await client().listBriefPushBindings()).toEqual([{ id: "b2", channel: "telegram", label: "Kept" }]);
  });
});

describe("ApiClient brief feedback", () => {
  it("POST /api/briefs/:id/feedback sends the rating, tags and note", async () => {
    const fetchMock = respond({
      id: "f1",
      brief_id: "b1",
      rating: "not_useful",
      tags: ["too_long"],
      note: "Shorter please",
      created_at: "t",
      updated_at: "t",
    });
    const saved = await client().submitBriefFeedback("b1", {
      rating: "not_useful",
      tags: ["too_long"],
      note: "Shorter please",
    });
    expect(saved).toMatchObject({ rating: "not_useful", tags: ["too_long"], note: "Shorter please" });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/briefs/b1/feedback");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
  });

  it("GET /api/briefs/:id/feedback reads null when the caller has not rated", async () => {
    respond(null);
    expect(await client().getMyBriefFeedback("b1")).toBeNull();
  });

  it("reads the usefulness summary, and throws a 403 for a member who may not see it", async () => {
    respond({ days: 14, ratings: 3, useful: 2, usefulness: 2 / 3 });
    expect(await client().getBriefFeedbackSummary()).toMatchObject({ ratings: 3, useful: 2 });

    respond({ error: "forbidden" }, 403);
    const error = await client().getBriefFeedbackSummary().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(403);
  });
});

describe("ApiClient squad attention", () => {
  const attention = {
    squad_id: "s1",
    pending_cap: 3,
    weekly_cap: 3,
    computed_pending_cap: 3,
    computed_weekly_cap: 3,
    pinned: false,
    pinned_pending_cap: null,
    pinned_weekly_cap: null,
    acceptance_rate: null,
    sample_size: 2,
    reason: "sample_too_small",
    computed_at: null,
  };

  it("GET /api/squads/:id/attention reads the caps and reason", async () => {
    const fetchMock = respond(attention);
    expect(await client().getSquadAttention("s1")).toMatchObject({ reason: "sample_too_small", pending_cap: 3 });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/squads/s1/attention");
  });

  it("PUT /api/squads/:id/attention/pin sends the pin and reads the new caps", async () => {
    const fetchMock = respond({ ...attention, pinned: true, pending_cap: 1, weekly_cap: 2, reason: "pinned" });
    const saved = await client().setSquadAttentionPin("s1", { pinned: true, pending_cap: 1, weekly_cap: 2 });
    expect(saved).toMatchObject({ pinned: true, reason: "pinned" });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/squads/s1/attention/pin");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      pinned: true,
      pending_cap: 1,
      weekly_cap: 2,
    });
  });
});
