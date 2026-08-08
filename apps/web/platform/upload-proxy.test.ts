import { describe, expect, it, vi } from "vitest";
import { proxyUpload } from "./upload-proxy";

describe("proxyUpload", () => {
  it("forwards the multipart stream and preserves the backend response", async () => {
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("multipart bytes"));
        controller.close();
      },
    });
    const request = new Request("https://app.test/api/upload-file?draft=1", {
      method: "POST",
      headers: {
        authorization: "Bearer token",
        "content-type": "multipart/form-data; boundary=test",
        "content-length": "15",
        "x-workspace-slug": "acme",
      },
      body,
      duplex: "half",
    } as RequestInit & { duplex: "half" });
    const fetchImpl = vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
      expect(init?.body).toBe(body);
      expect(new Headers(init?.headers).get("authorization")).toBe("Bearer token");
      expect(new Headers(init?.headers).get("x-workspace-slug")).toBe("acme");
      expect(new Headers(init?.headers).has("content-length")).toBe(false);
      return new Response("uploaded", {
        status: 201,
        headers: { "x-upload-id": "att-1" },
      });
    });

    const response = await proxyUpload(
      request,
      { NODE_ENV: "production", REMOTE_API_URL: "http://backend:8080/base" },
      fetchImpl,
    );

    expect(fetchImpl).toHaveBeenCalledOnce();
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe(
      "http://backend:8080/base/api/upload-file?draft=1",
    );
    expect(response.status).toBe(201);
    expect(response.headers.get("x-upload-id")).toBe("att-1");
    await expect(response.text()).resolves.toBe("uploaded");
  });

  it("returns 503 when the production backend is not configured", async () => {
    const response = await proxyUpload(
      new Request("https://app.test/api/upload-file", { method: "POST" }),
      { NODE_ENV: "production" },
    );

    expect(response.status).toBe(503);
    await expect(response.json()).resolves.toMatchObject({
      error: "file upload backend is not configured",
    });
  });

  it("turns upstream network errors into a stable 502", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error("connection refused");
    });
    const response = await proxyUpload(
      new Request("https://app.test/api/upload-file", { method: "POST" }),
      { NODE_ENV: "production", REMOTE_API_URL: "http://backend:8080" },
      fetchImpl,
    );

    expect(response.status).toBe(502);
  });
});
