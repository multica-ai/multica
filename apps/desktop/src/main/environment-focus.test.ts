// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { focusEnvironmentAt } from "./environment-focus";

function respond(status: number, body: string, contentType = "application/json") {
  return vi.fn(async () => new Response(body, { status, headers: { "Content-Type": contentType } }));
}

describe("focusEnvironmentAt", () => {
  it("posts the key as JSON to the daemon's focus endpoint", async () => {
    const fetchImpl = respond(200, `{"focused":true}`);
    await expect(focusEnvironmentAt(19514, "detected:abc", fetchImpl)).resolves.toEqual({ ok: true });
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://127.0.0.1:19514/environments/focus");
    expect(init.method).toBe("POST");
    expect(init.headers).toEqual({ "Content-Type": "application/json" });
    expect(JSON.parse(init.body as string)).toEqual({ key: "detected:abc" });
  });

  it("passes on why the daemon could not bring the app forward", async () => {
    await expect(
      focusEnvironmentAt(1, "k", respond(404, `{"reason":"gone","error":"the app is no longer running"}`)),
    ).resolves.toEqual({ ok: false, reason: "gone", error: "the app is no longer running" });
    await expect(focusEnvironmentAt(1, "k", respond(501, `{"reason":"unsupported"}`))).resolves.toEqual({
      ok: false,
      reason: "unsupported",
    });
    await expect(focusEnvironmentAt(1, "k", respond(403, `{"reason":"forbidden","error":"no"}`))).resolves.toEqual({
      ok: false,
      reason: "failed",
      error: "no",
    });
  });

  it("treats a daemon without the endpoint as unsupported", async () => {
    await expect(focusEnvironmentAt(1, "k", respond(404, "404 page not found\n", "text/plain"))).resolves.toEqual({
      ok: false,
      reason: "unsupported",
    });
  });

  it("reports an unreachable daemon as unavailable", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    await expect(focusEnvironmentAt(1, "k", fetchImpl)).resolves.toEqual({ ok: false, reason: "unavailable" });
  });
});
