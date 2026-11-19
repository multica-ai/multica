// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

const client = () => new ApiClient("https://api.example.test");

const scheduled = {
  id: "upd-1",
  runtime_id: "rt-1",
  status: "scheduled",
  target_version: "v0.9.2",
  scheduled_at: "2026-11-19T06:32:00Z",
  active_tasks: 2,
  created_at: "2026-11-19T06:32:00Z",
  updated_at: "2026-11-19T06:32:05Z",
};

describe("runtime update API", () => {
  it("parses a queued update with the fields the update section renders", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(scheduled)));
    await expect(client().getUpdateResult("rt-1", "upd-1")).resolves.toMatchObject({
      status: "scheduled",
      scheduled_at: "2026-11-19T06:32:00Z",
      active_tasks: 2,
    });
  });

  // A daemon that predates queued updates cannot count tasks; a garbled count
  // must read as "unknown", never as a number the UI would print.
  it.each([undefined, -1, 1.5, "2", null])(
    "drops an unusable active task count: %j",
    async (activeTasks) => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(Response.json({ ...scheduled, active_tasks: activeTasks })),
      );
      const update = await client().getUpdateResult("rt-1", "upd-1");
      expect(update.status).toBe("scheduled");
      expect(update.active_tasks).toBeUndefined();
    },
  );

  it("keeps a status from a newer server instead of rewriting it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ ...scheduled, status: "draining" })),
    );
    const update = await client().getUpdateResult("rt-1", "upd-1");
    expect(update.status).toBe("draining");
  });

  it.each([null, {}, { status: "scheduled" }, { id: 7, status: "scheduled" }])(
    "turns a malformed record into an explicit failure: %j",
    async (body) => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
      const update = await client().getUpdateResult("rt-1", "upd-1");
      expect(update).toMatchObject({ id: "upd-1", runtime_id: "rt-1", status: "failed" });
      expect(update.error).toBeTruthy();
    },
  );

  it("reads the active update from its envelope", async () => {
    const fetch = vi.fn().mockResolvedValue(Response.json({ update: scheduled }));
    vi.stubGlobal("fetch", fetch);
    await expect(client().getActiveUpdate("rt-1")).resolves.toMatchObject({ id: "upd-1" });
    const [url, init] = fetch.mock.calls[0]!;
    expect(new URL(url).pathname).toBe("/api/runtimes/rt-1/update");
    expect(init.method ?? "GET").toBe("GET");
  });

  it.each([{ update: null }, {}, { update: { status: "scheduled" } }, null])(
    "treats an empty or malformed active envelope as no active update: %j",
    async (body) => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
      await expect(client().getActiveUpdate("rt-1")).resolves.toBeNull();
    },
  );

  // Before the endpoint existed the path only had POST, so chi answers 405;
  // a server that routes it elsewhere answers 404. Neither is an error here.
  it.each([404, 405])("treats %i from an older server as no active update", async (status) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ error: "nope" }, { status })),
    );
    await expect(client().getActiveUpdate("rt-1")).resolves.toBeNull();
  });

  it("surfaces other failures of the active endpoint", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ error: "boom" }, { status: 500 })),
    );
    await expect(client().getActiveUpdate("rt-1")).rejects.toThrow();
  });

  it("cancels through the update's cancel route", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(Response.json({ ...scheduled, status: "cancelled" }));
    vi.stubGlobal("fetch", fetch);
    await expect(client().cancelUpdate("rt-1", "upd-1")).resolves.toMatchObject({
      status: "cancelled",
    });
    const [url, init] = fetch.mock.calls[0]!;
    expect(new URL(url).pathname).toBe("/api/runtimes/rt-1/update/upd-1/cancel");
    expect(init.method).toBe("POST");
  });
});
