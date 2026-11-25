// @vitest-environment node

import { createServer } from "node:http";
import { afterEach, describe, expect, it, vi } from "vitest";
import { proxyUpload } from "./upload-proxy";

const production = { NODE_ENV: "production", REMOTE_API_URL: "http://backend:8080" };

afterEach(() => vi.restoreAllMocks());

describe("proxyUpload", () => {
  it("uploads a 2 MiB multipart body with Expect: 100-continue through real fetch", async () => {
    let receivedBody: Buffer | undefined;
    let receivedExpect: string | undefined;
    const upstream = createServer((request, response) => {
      receivedExpect = request.headers.expect;
      const chunks: Buffer[] = [];
      request.on("data", (chunk: Buffer) => chunks.push(chunk));
      request.on("end", () => {
        receivedBody = Buffer.concat(chunks);
        response.end("uploaded");
      });
    });
    await new Promise<void>((resolve, reject) => {
      upstream.once("error", reject);
      upstream.listen(0, "127.0.0.1", resolve);
    });

    try {
      const address = upstream.address();
      if (!address || typeof address === "string") throw new Error("missing upstream port");
      const form = new FormData();
      form.append("file", new Blob([new Uint8Array(2 * 1024 * 1024)]), "attachment.bin");
      const request = new Request("https://app.test/api/upload-file", {
        method: "POST",
        headers: { Expect: "100-continue" },
        body: form,
      });
      const expectedBody = Buffer.from(await request.clone().arrayBuffer());
      const response = await proxyUpload(request, {
        ...production,
        REMOTE_API_URL: `http://127.0.0.1:${address.port}`,
      });

      expect(response.status).toBe(200);
      expect(await response.text()).toBe("uploaded");
      expect(receivedExpect).toBeUndefined();
      expect(receivedBody?.equals(expectedBody)).toBe(true);
    } finally {
      await new Promise<void>((resolve, reject) => {
        upstream.close((error) => error ? reject(error) : resolve());
        upstream.closeAllConnections();
      });
    }
  });

  it("forwards the untouched multipart stream, credentials, workspace, and query", async () => {
    const body = new ReadableStream<Uint8Array>();
    const request = new Request("https://app.test/api/upload-file?draft=1", {
      method: "POST",
      headers: {
        authorization: "Bearer test-token",
        cookie: "multica_session=test-session",
        "x-csrf-token": "test-csrf",
        "x-workspace-slug": "acme",
        "content-type": "multipart/form-data; boundary=test",
        "content-length": "100",
        connection: "keep-alive, x-hop-only",
        "x-hop-only": "discard",
      },
      body,
      duplex: "half",
    } as RequestInit & { duplex: "half" });
    const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
      expect(init?.method).toBe("POST");
      expect(init?.body).toBe(request.body);
      expect(init?.signal).toBe(request.signal);
      expect(init?.redirect).toBe("manual");
      const headers = new Headers(init?.headers);
      for (const name of ["authorization", "cookie", "x-csrf-token", "x-workspace-slug", "content-type"]) {
        expect(headers.get(name)).toBe(request.headers.get(name));
      }
      for (const name of ["content-length", "connection", "x-hop-only"]) {
        expect(headers.has(name)).toBe(false);
      }
      expect(headers.get("accept-encoding")).toBe("identity");
      expect(headers.get("x-forwarded-host")).toBe("app.test");
      return new Response("uploaded", { status: 201, headers: { "x-upload-id": "att-1" } });
    });
    const response = await proxyUpload(request, { ...production, REMOTE_API_URL: "http://backend:8080/base" }, fetchImpl);
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe("http://backend:8080/base/api/upload-file?draft=1");
    expect(request.bodyUsed).toBe(false);
    expect(response.status).toBe(201);
    expect(response.headers.get("x-upload-id")).toBe("att-1");
    expect(await response.text()).toBe("uploaded");
  });

  it.each([401, 403, 413, 500])("preserves the backend's %i response and structured body", async (status) => {
    const body = { error: "backend rejection", code: "test_code", max_size_bytes: 104857600 };
    const response = await proxyUpload(
      new Request("https://app.test/api/upload-file", { method: "POST" }),
      production,
      vi.fn().mockResolvedValue(Response.json(body, { status })),
    );
    expect(response.status).toBe(status);
    expect(await response.json()).toEqual(body);
  });

  it("preserves multiple cookies but removes hop headers and decoded-response framing", async () => {
    const headers = new Headers({
      connection: "x-hop-only",
      "x-hop-only": "discard",
      "content-encoding": "gzip",
      "content-length": "45",
      "content-type": "application/json",
    });
    headers.append("set-cookie", "first=1; HttpOnly");
    headers.append("set-cookie", "second=2; HttpOnly");
    // Node fetch exposes decoded body bytes while retaining the wire headers.
    const response = await proxyUpload(
      new Request("https://app.test/api/upload-file", { method: "POST" }),
      production,
      vi.fn().mockResolvedValue(new Response('{"ok":true}', { headers })),
    );
    expect(response.headers.getSetCookie()).toEqual(["first=1; HttpOnly", "second=2; HttpOnly"]);
    for (const name of ["connection", "x-hop-only", "content-encoding", "content-length"]) {
      expect(response.headers.has(name)).toBe(false);
    }
    expect(await response.json()).toEqual({ ok: true });
  });

  it("forwards CORS preflight to the existing backend", async () => {
    const request = new Request("https://app.test/api/upload-file", {
      method: "OPTIONS",
      headers: { origin: "https://client.test", "access-control-request-method": "POST" },
    });
    const fetchImpl = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
      expect(init?.method).toBe("OPTIONS");
      expect(new Headers(init?.headers).get("origin")).toBe("https://client.test");
      return new Response(null, { status: 204, headers: { "access-control-allow-origin": "https://client.test" } });
    });
    const response = await proxyUpload(request, production, fetchImpl);
    expect(response.status).toBe(204);
    expect(response.headers.get("access-control-allow-origin")).toBe("https://client.test");
  });

  it("returns 503 without fetching if the production backend is not configured", async () => {
    const fetchImpl = vi.fn();
    const response = await proxyUpload(new Request("https://app.test/api/upload-file"), { NODE_ENV: "production" }, fetchImpl);
    expect(response.status).toBe(503);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("uses the existing development backend resolver", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(Response.json({ ok: true }));
    await proxyUpload(new Request("https://app.test/api/upload-file"), { NODE_ENV: "development", BACKEND_PORT: "18080" }, fetchImpl);
    expect(String(fetchImpl.mock.calls[0]?.[0])).toBe("http://localhost:18080/api/upload-file");
  });

  it("returns a stable 502 for an unreachable backend", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const response = await proxyUpload(
      new Request("https://app.test/api/upload-file"), production,
      vi.fn().mockRejectedValue(new Error("connection refused")),
    );
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ error: "file upload backend unavailable" });
  });

  it("propagates request cancellation to the upstream fetch", async () => {
    const controller = new AbortController();
    const request = new Request("https://app.test/api/upload-file", { signal: controller.signal });
    const fetchImpl = vi.fn((_url: string | URL | Request, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(init.signal?.reason), { once: true });
    }));
    const response = proxyUpload(request, production, fetchImpl);
    controller.abort(new DOMException("user cancelled", "AbortError"));
    await expect(response).rejects.toThrow("user cancelled");
    expect(fetchImpl.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  });
});
