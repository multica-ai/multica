import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchDaemonHealth } from "./daemon-health";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("daemon health", () => {
  it("accepts only the public contract and rejects redirects", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('{"status":"running","os":"darwin"}', { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    expect(await fetchDaemonHealth(19514)).toEqual({ status: "running", os: "darwin" });
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:19514/health", {
      redirect: "error",
      signal: expect.any(AbortSignal),
    });
  });

  it("rejects oversized, ambiguous, trailing, and unknown responses", async () => {
    for (const response of [
      new Response("{}", {
        status: 200,
        headers: { "Content-Length": String((1 << 20) + 1) },
      }),
      new Response(`{"status":"running","os":"${"x".repeat(1 << 20)}"}`, {
        status: 200,
      }),
      new Response('{"status":"running","status":"starting"}', { status: 200 }),
      new Response('{"status":"running"} true', { status: 200 }),
      new Response('{"status":"running","pid":123}', { status: 200 }),
      new Response('{"status":"running"}', { status: 200 }),
    ]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
      expect(await fetchDaemonHealth(19514)).toBeNull();
    }
  });

  it("keeps the timeout active while reading the response body", async () => {
    vi.useFakeTimers();
    try {
      const body = new ReadableStream<Uint8Array>({ start() {} });
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(new Response(body, { status: 200 })),
      );

      const result = fetchDaemonHealth(19514);
      await vi.advanceTimersByTimeAsync(2_001);
      await expect(result).resolves.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });
});
